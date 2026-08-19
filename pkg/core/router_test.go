package core

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"network-router/pkg/utils"
)

type MockRouteManager struct {
	mu             sync.Mutex
	addedRoutes    []string
	addedGateways  []string
	deletedRoutes  []string
	addErr         error
	addExistsFirst bool
}

func NewMockRouteManager() *MockRouteManager {
	return &MockRouteManager{}
}

func (m *MockRouteManager) AddRouteViaGateway(destination string, gatewayIP string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.addErr != nil {
		return m.addErr
	}
	if m.addExistsFirst {
		m.addExistsFirst = false
		m.addedRoutes = append(m.addedRoutes, destination)
		m.addedGateways = append(m.addedGateways, gatewayIP)
		return utils.ErrRouteExists
	}
	m.addedRoutes = append(m.addedRoutes, destination)
	m.addedGateways = append(m.addedGateways, gatewayIP)
	return nil
}

func (m *MockRouteManager) DeleteRoute(destination string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedRoutes = append(m.deletedRoutes, destination)
	return nil
}

func (m *MockRouteManager) routes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.addedRoutes...)
}

func (m *MockRouteManager) gateways() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.addedGateways...)
}

func (m *MockRouteManager) deleted() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.deletedRoutes...)
}

func phoneProvider() *fakeNetworkProvider {
	return &fakeNetworkProvider{
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
}

func newTestRouter(t *testing.T, config *Config, provider NetworkProvider, manager RouteManager) (*Router, *RouteApplier) {
	t.Helper()
	snapshots := NewSnapshotStore()
	applier := NewRouteApplier(manager, snapshots, filepath.Join(t.TempDir(), "state.yaml"))
	return NewRouter(config, provider, snapshots, applier), applier
}

func TestApplyRoutesUsesCurrentPhoneGatewayAfterDeviceRemap(t *testing.T) {
	provider := phoneProvider()
	manager := NewMockRouteManager()
	// Wildcard-only domains keep this test off the network.
	router, _ := newTestRouter(t, &Config{
		TetherCIDRs:       []string{"192.0.2.0/24"},
		TetherDomains:     []string{"*.example.com"},
		DNSUpstreamPinned: []string{"1.1.1.1:53"},
	}, provider, manager)

	provider.interfaces[1].DeviceName = "en8"
	provider.addresses["en8"] = provider.addresses["en7"]
	provider.gateways["en8"] = "172.20.10.9"

	if err := router.ApplyRoutes(); err != nil {
		t.Fatalf("apply routes: %v", err)
	}

	for _, gateway := range manager.gateways() {
		if gateway != "172.20.10.9" {
			t.Fatalf("expected every route via the remapped gateway, got %v", manager.gateways())
		}
	}
	if !contains(manager.routes(), "192.0.2.0/24") {
		t.Fatalf("expected the configured CIDR to be routed, got %v", manager.routes())
	}
}

func TestApplyRoutesPinsUpstreamResolversToTheTetheredPath(t *testing.T) {
	manager := NewMockRouteManager()
	router, _ := newTestRouter(t, &Config{
		TetherCIDRs:       []string{"192.0.2.0/24"},
		DNSUpstreamPinned: []string{"1.1.1.1:53", "8.8.8.8:53"},
	}, phoneProvider(), manager)

	if err := router.ApplyRoutes(); err != nil {
		t.Fatalf("apply routes: %v", err)
	}

	// Without these host routes the proxy would reach its fallback resolvers
	// over the default route, which is the network being bypassed.
	for _, expected := range []string{"1.1.1.1/32", "8.8.8.8/32"} {
		if !contains(manager.routes(), expected) {
			t.Fatalf("expected pinned resolver route %s, got %v", expected, manager.routes())
		}
	}
}

func TestApplyRoutesSkipsWildcardBaseDomains(t *testing.T) {
	manager := NewMockRouteManager()
	router, _ := newTestRouter(t, &Config{
		TetherCIDRs:   []string{"192.0.2.0/24"},
		TetherDomains: []string{"*.googlevideo.com"},
	}, phoneProvider(), manager)

	if err := router.ApplyRoutes(); err != nil {
		t.Fatalf("apply routes: %v", err)
	}

	// Resolving googlevideo.com produces an address no real subdomain uses.
	// Only the CIDR and the pinned resolvers should be present.
	for _, route := range manager.routes() {
		if route == "192.0.2.0/24" {
			continue
		}
		if route == "1.1.1.1/32" || route == "8.8.8.8/32" {
			continue
		}
		t.Fatalf("unexpected route derived from a wildcard base domain: %s", route)
	}
}

func TestApplyRoutesFailsWithoutUsablePhonePath(t *testing.T) {
	provider := phoneProvider()
	provider.addresses["en7"] = []string{"169.254.134.156"}
	delete(provider.gateways, "en7")

	manager := NewMockRouteManager()
	router, _ := newTestRouter(t, &Config{TetherCIDRs: []string{"192.0.2.0/24"}}, provider, manager)

	if err := router.ApplyRoutes(); err == nil {
		t.Fatal("expected apply to fail without a usable phone path")
	}
	if len(manager.routes()) != 0 {
		t.Fatalf("expected no routes, got %v", manager.routes())
	}
}

func TestApplyRoutesToleratesRoutesThatAlreadyExist(t *testing.T) {
	manager := NewMockRouteManager()
	manager.addExistsFirst = true
	router, applier := newTestRouter(t, &Config{
		TetherCIDRs:       []string{"192.0.2.0/24"},
		DNSUpstreamPinned: []string{"1.1.1.1:53"},
	}, phoneProvider(), manager)

	// An entry already in the table is the desired state, so it must not deny
	// the rest of the batch.
	if err := router.ApplyRoutes(); err != nil {
		t.Fatalf("apply routes must tolerate an existing entry: %v", err)
	}
	if applier.Count() == 0 {
		t.Fatal("expected the pre-existing route to be tracked for cleanup")
	}
}

func TestClearRoutesRemovesEverythingInstalled(t *testing.T) {
	manager := NewMockRouteManager()
	router, applier := newTestRouter(t, &Config{
		TetherCIDRs:       []string{"192.0.2.0/24"},
		DNSUpstreamPinned: []string{"1.1.1.1:53"},
	}, phoneProvider(), manager)

	if err := router.ApplyRoutes(); err != nil {
		t.Fatalf("apply routes: %v", err)
	}
	installed := len(manager.routes())

	if err := router.ClearRoutes(); err != nil {
		t.Fatalf("clear routes: %v", err)
	}
	if len(manager.deleted()) != installed {
		t.Fatalf("expected %d deletions, got %v", installed, manager.deleted())
	}
	if applier.Count() != 0 {
		t.Fatalf("expected no tracked routes after clear, got %d", applier.Count())
	}
}

func TestApplyRoutesReportsFailureWhenNothingCanBeInstalled(t *testing.T) {
	manager := NewMockRouteManager()
	manager.addErr = errors.New("route rejected")
	router, _ := newTestRouter(t, &Config{TetherCIDRs: []string{"192.0.2.0/24"}}, phoneProvider(), manager)

	if err := router.ApplyRoutes(); err == nil {
		t.Fatal("expected apply to fail when every route is rejected")
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
