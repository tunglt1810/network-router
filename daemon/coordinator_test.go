package daemon

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"network-router/pkg/core"
	"network-router/pkg/utils"
)

type coordinatorNetworkProvider struct {
	interfaces []utils.InterfaceInfo
	addresses  map[string][]string
	gateways   map[string]string
}

func (p *coordinatorNetworkProvider) GetNetworkInterfaces() ([]utils.InterfaceInfo, error) {
	return p.interfaces, nil
}

func (p *coordinatorNetworkProvider) GetInterfaceIPv4Addresses(deviceName string) ([]string, error) {
	return p.addresses[deviceName], nil
}

func (p *coordinatorNetworkProvider) GetInterfaceGateway(deviceName string) (string, error) {
	return p.gateways[deviceName], nil
}

type coordinatorRouteManager struct {
	mu            sync.Mutex
	addErr        error
	addedRoutes   []string
	deletedRoutes []string
}

func (m *coordinatorRouteManager) AddRouteViaGateway(destination string, gatewayIP string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.addErr != nil {
		return m.addErr
	}
	m.addedRoutes = append(m.addedRoutes, destination)
	return nil
}

func (m *coordinatorRouteManager) DeleteRoute(destination string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedRoutes = append(m.deletedRoutes, destination)
	return nil
}

func (m *coordinatorRouteManager) deletedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.deletedRoutes)
}

type fakeDNSController struct {
	mu     sync.Mutex
	starts int
	stops  int
}

func (d *fakeDNSController) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.starts++
	return nil
}

func (d *fakeDNSController) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stops++
	return nil
}

func (d *fakeDNSController) counts() (int, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.starts, d.stops
}

type coordinatorFixture struct {
	coordinator *Coordinator
	provider    *coordinatorNetworkProvider
	manager     *coordinatorRouteManager
	dns         *fakeDNSController
	snapshots   *core.SnapshotStore
}

func newCoordinatorFixture(t *testing.T) *coordinatorFixture {
	t.Helper()

	provider := &coordinatorNetworkProvider{
		interfaces: []utils.InterfaceInfo{
			{Name: "Wi-Fi", DeviceName: "en0"},
			{Name: "iPhone USB", DeviceName: "en7"},
		},
		addresses: map[string][]string{
			"en0": {"10.0.0.2"},
			"en7": {"172.20.10.2"},
		},
		gateways: map[string]string{
			"en0": "10.0.0.1",
			"en7": "172.20.10.1",
		},
	}

	config := &core.Config{
		TetherCIDRs:       []string{"192.0.2.0/24"},
		DNSUpstreamPinned: []string{"1.1.1.1:53"},
	}
	manager := &coordinatorRouteManager{}
	dns := &fakeDNSController{}
	snapshots := core.NewSnapshotStore()
	applier := core.NewRouteApplier(manager, snapshots, filepath.Join(t.TempDir(), "state.yaml"))
	router := core.NewRouter(config, provider, snapshots, applier)
	coordinator := NewCoordinator(config, router, applier, dns, snapshots, make(chan *core.NetworkSnapshot))

	return &coordinatorFixture{
		coordinator: coordinator,
		provider:    provider,
		manager:     manager,
		dns:         dns,
		snapshots:   snapshots,
	}
}

func readySnapshot(device, gateway string) *core.NetworkSnapshot {
	return &core.NetworkSnapshot{
		WifiInterface:  &utils.InterfaceInfo{Name: "Wi-Fi", DeviceName: "en0"},
		PhoneInterface: &utils.InterfaceInfo{Name: "iPhone USB", DeviceName: device},
		WifiActive:     true,
		WifiGateway:    "10.0.0.1",
		PhoneGateway:   gateway,
		PhoneReady:     true,
	}
}

func TestCoordinatorClearsAndStopsServicesWhenPhoneIsLost(t *testing.T) {
	fixture := newCoordinatorFixture(t)

	fixture.coordinator.handleSnapshot(readySnapshot("en7", "172.20.10.1"))
	if !fixture.coordinator.GetStatus().RoutesApplied {
		t.Fatal("expected routes to be applied")
	}

	fixture.coordinator.handleSnapshot(&core.NetworkSnapshot{
		WifiInterface: &utils.InterfaceInfo{Name: "Wi-Fi", DeviceName: "en0"},
		WifiActive:    true,
		PhoneReady:    false,
		PhoneReason:   "phone interface has no gateway",
	})

	if fixture.coordinator.GetStatus().RoutesApplied {
		t.Fatal("expected routes to be cleared")
	}
	if _, stops := fixture.dns.counts(); stops == 0 {
		t.Fatal("expected DNS controller to stop")
	}
	if fixture.manager.deletedCount() == 0 {
		t.Fatal("expected route cleanup")
	}
}

func TestCoordinatorReappliesWhenPhoneGatewayChanges(t *testing.T) {
	fixture := newCoordinatorFixture(t)
	fixture.coordinator.handleSnapshot(readySnapshot("en7", "172.20.10.1"))

	fixture.provider.interfaces[1].DeviceName = "en8"
	fixture.provider.addresses["en8"] = fixture.provider.addresses["en7"]
	fixture.provider.gateways["en8"] = "172.20.10.9"
	fixture.coordinator.handleSnapshot(readySnapshot("en8", "172.20.10.9"))

	status := fixture.coordinator.GetStatus()
	if !status.RoutesApplied {
		t.Fatal("expected routes to remain applied after rebind")
	}
	if status.PhoneDevice != "en8" {
		t.Fatalf("expected status to report en8, got %s", status.PhoneDevice)
	}
	starts, stops := fixture.dns.counts()
	if starts < 2 || stops < 1 {
		t.Fatalf("expected the DNS proxy to be restarted, starts=%d stops=%d", starts, stops)
	}
	if fixture.manager.deletedCount() == 0 {
		t.Fatal("expected old routes to be cleared")
	}
}

func TestCoordinatorDoesNotMarkRoutesAppliedWhenApplyFails(t *testing.T) {
	fixture := newCoordinatorFixture(t)
	fixture.manager.addErr = errors.New("route rejected")

	fixture.coordinator.handleSnapshot(readySnapshot("en7", "172.20.10.1"))

	if fixture.coordinator.GetStatus().RoutesApplied {
		t.Fatal("routes must remain unapplied after failure")
	}
	if starts, _ := fixture.dns.counts(); starts != 0 {
		t.Fatalf("DNS controller must not start after apply failure, starts=%d", starts)
	}
}

// GetStatus must reflect the published snapshot rather than a private copy, so
// reported state cannot drift from what routing decisions actually used.
func TestCoordinatorStatusReadsFromSnapshotStore(t *testing.T) {
	fixture := newCoordinatorFixture(t)
	fixture.snapshots.Store(readySnapshot("en9", "172.20.10.5"))

	status := fixture.coordinator.GetStatus()
	if status.PhoneDevice != "en9" || status.PhoneGateway != "172.20.10.5" {
		t.Fatalf("status did not follow the snapshot store: %#v", status)
	}
}
