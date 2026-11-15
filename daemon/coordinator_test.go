package daemon

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"network-router/pkg/core"
	"network-router/pkg/utils"
)

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
	mu      sync.Mutex
	starts  int
	stops   int
	running bool
}

func (d *fakeDNSController) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.starts++
	d.running = true
	return nil
}

func (d *fakeDNSController) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stops++
	d.running = false
	return nil
}

func (d *fakeDNSController) Running() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running
}

func (d *fakeDNSController) counts() (int, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.starts, d.stops
}

type coordinatorFixture struct {
	coordinator *Coordinator
	manager     *coordinatorRouteManager
	dns         *fakeDNSController
	snapshots   *core.SnapshotStore
}

func newCoordinatorFixture(t *testing.T) *coordinatorFixture {
	t.Helper()

	config := &core.Config{
		TetherCIDRs:       []string{"192.0.2.0/24"},
		DNSUpstreamPinned: []string{"1.1.1.1:53"},
	}
	manager := &coordinatorRouteManager{}
	dns := &fakeDNSController{}
	snapshots := core.NewSnapshotStore()
	applier := core.NewRouteApplier(manager, snapshots, filepath.Join(t.TempDir(), "state.yaml"))
	router := core.NewRouter(config, applier)
	coordinator := NewCoordinator(router, applier, dns, snapshots, make(chan *core.NetworkSnapshot))

	return &coordinatorFixture{
		coordinator: coordinator,
		manager:     manager,
		dns:         dns,
		snapshots:   snapshots,
	}
}

// observe publishes a snapshot and hands it to the coordinator, as the
// detector does.
func (f *coordinatorFixture) observe(snapshot *core.NetworkSnapshot) {
	f.snapshots.Store(snapshot)
	f.coordinator.handleSnapshot(snapshot)
}

func readySnapshot(device, gateway string) *core.NetworkSnapshot {
	return &core.NetworkSnapshot{
		PhoneInterface: &utils.InterfaceInfo{Name: "iPhone USB", DeviceName: device},
		PhoneGateway:   gateway,
		PhoneReady:     true,
	}
}

func TestCoordinatorClearsAndStopsServicesWhenPhoneIsLost(t *testing.T) {
	fixture := newCoordinatorFixture(t)

	fixture.observe(readySnapshot("en7", "172.20.10.1"))
	if !fixture.coordinator.GetStatus().RoutesApplied {
		t.Fatal("expected routes to be applied")
	}

	fixture.observe(&core.NetworkSnapshot{
		PhoneReady:  false,
		PhoneReason: "phone interface has no gateway",
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
	fixture.observe(readySnapshot("en7", "172.20.10.1"))

	fixture.observe(readySnapshot("en8", "172.20.10.9"))

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

	fixture.observe(readySnapshot("en7", "172.20.10.1"))

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

func notReadySnapshot() *core.NetworkSnapshot {
	return &core.NetworkSnapshot{
		PhoneReady:  false,
		PhoneReason: "phone interface has no gateway",
	}
}

// Regression: on after off must turn routing back on, so the next detector
// tick does not tear the routes down again.
func TestOnAfterClearKeepsRoutes(t *testing.T) {
	f := newCoordinatorFixture(t)
	ready := readySnapshot("en7", "172.20.10.1")
	f.snapshots.Store(ready)
	_ = f.coordinator.Off()

	applied, err := f.coordinator.On()
	if err != nil || !applied {
		t.Fatalf("expected routes applied, applied=%v err=%v", applied, err)
	}
	status := f.coordinator.GetStatus()
	if !status.AutoRoutingEnabled || !status.RoutesApplied {
		t.Fatalf("expected auto-routing on and routes applied: %#v", status)
	}
	deletedBefore := f.manager.deletedCount()

	f.observe(ready)
	if !f.coordinator.GetStatus().RoutesApplied {
		t.Fatal("routes were cleared by the next snapshot")
	}
	if got := f.manager.deletedCount(); got != deletedBefore {
		t.Fatalf("routes deleted after snapshot: before=%d after=%d", deletedBefore, got)
	}

	if _, err := f.coordinator.On(); err != nil {
		t.Fatalf("second apply failed: %v", err)
	}
	f.observe(ready)
	if !f.coordinator.GetStatus().RoutesApplied {
		t.Fatal("routes were cleared after a second apply")
	}
}

func TestOnWaitsWhenTetheringNotReady(t *testing.T) {
	f := newCoordinatorFixture(t)
	_ = f.coordinator.Off()
	f.snapshots.Store(notReadySnapshot())

	applied, err := f.coordinator.On()
	if err != nil || applied {
		t.Fatalf("expected armed without error, applied=%v err=%v", applied, err)
	}
	status := f.coordinator.GetStatus()
	if !status.AutoRoutingEnabled || status.RoutesApplied {
		t.Fatalf("expected auto-routing on and no routes: %#v", status)
	}
	if starts, _ := f.dns.counts(); starts != 0 {
		t.Fatalf("DNS proxy must not start while waiting, starts=%d", starts)
	}
	f.manager.mu.Lock()
	added := len(f.manager.addedRoutes)
	f.manager.mu.Unlock()
	if added != 0 {
		t.Fatalf("no routes must be added while waiting, added=%d", added)
	}

	f.observe(readySnapshot("en7", "172.20.10.1"))
	if !f.coordinator.GetStatus().RoutesApplied {
		t.Fatal("expected routes once tethering became ready")
	}
}

func TestOnWaitsWhenNoSnapshot(t *testing.T) {
	f := newCoordinatorFixture(t)

	applied, err := f.coordinator.On()
	if err != nil || applied {
		t.Fatalf("expected armed without error, applied=%v err=%v", applied, err)
	}
	if !f.coordinator.GetStatus().AutoRoutingEnabled {
		t.Fatal("expected auto-routing on")
	}
}

func TestOnFailureKeepsAutoRoutingOn(t *testing.T) {
	f := newCoordinatorFixture(t)
	f.snapshots.Store(readySnapshot("en7", "172.20.10.1"))
	f.manager.addErr = errors.New("route rejected")
	_ = f.coordinator.Off()

	if _, err := f.coordinator.On(); err == nil {
		t.Fatal("expected apply error")
	}
	status := f.coordinator.GetStatus()
	if !status.AutoRoutingEnabled || status.RoutesApplied {
		t.Fatalf("expected auto-routing on and no routes: %#v", status)
	}
	if starts, _ := f.dns.counts(); starts != 0 {
		t.Fatalf("DNS proxy must not start after failure, starts=%d", starts)
	}
}

func TestOffClearsRoutesAndKeepsThemOff(t *testing.T) {
	f := newCoordinatorFixture(t)
	ready := readySnapshot("en7", "172.20.10.1")
	f.snapshots.Store(ready)
	if applied, err := f.coordinator.On(); err != nil || !applied {
		t.Fatalf("setup apply failed: applied=%v err=%v", applied, err)
	}

	if err := f.coordinator.Off(); err != nil {
		t.Fatalf("off: %v", err)
	}
	if f.manager.deletedCount() == 0 || f.dns.Running() {
		t.Fatal("expected off to clear routes and stop the DNS proxy")
	}

	f.observe(ready)
	if f.coordinator.GetStatus().RoutesApplied {
		t.Fatal("the next snapshot must not apply routes while routing is off")
	}
}

// IPC commands run on their own goroutines while handleSnapshot runs on the
// coordinator loop. Interleaved apply/clear must not leave the reported state
// out of sync with the routes that are actually installed.
func TestCoordinatorSerializesConcurrentOperations(t *testing.T) {
	f := newCoordinatorFixture(t)
	ready := readySnapshot("en7", "172.20.10.1")
	f.snapshots.Store(ready)

	ops := []func(){
		func() { _, _ = f.coordinator.On() },
		func() { _ = f.coordinator.Off() },
		func() { f.observe(ready) },
		func() { _ = f.coordinator.Refresh() },
	}
	var wg sync.WaitGroup
	for _, op := range ops {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				op()
			}
		}()
	}
	wg.Wait()

	status := f.coordinator.GetStatus()
	if status.RoutesApplied != (status.ActiveRoutes > 0) {
		t.Fatalf("state drift: RoutesApplied=%v ActiveRoutes=%d", status.RoutesApplied, status.ActiveRoutes)
	}
}

// On with a moved phone path must rebind the routes, not keep the old ones.
func TestOnRebindsWhenThePathMoved(t *testing.T) {
	f := newCoordinatorFixture(t)
	f.snapshots.Store(readySnapshot("en7", "172.20.10.1"))
	if applied, err := f.coordinator.On(); err != nil || !applied {
		t.Fatalf("setup apply failed: applied=%v err=%v", applied, err)
	}

	f.snapshots.Store(readySnapshot("en7", "172.20.10.9"))
	if applied, err := f.coordinator.On(); err != nil || !applied {
		t.Fatalf("second on failed: applied=%v err=%v", applied, err)
	}
	if f.manager.deletedCount() == 0 {
		t.Fatal("expected routes to be rebound to the new gateway")
	}

	deletedBefore := f.manager.deletedCount()
	f.observe(readySnapshot("en7", "172.20.10.9"))
	if f.manager.deletedCount() != deletedBefore {
		t.Fatal("the next snapshot must not rebind routes again")
	}
}

func TestRefreshFailsWhenRoutesAreNotApplied(t *testing.T) {
	fixture := newCoordinatorFixture(t)
	if err := fixture.coordinator.Refresh(); err == nil {
		t.Fatal("expected refresh to fail without applied routes")
	}
	if fixture.manager.deletedCount() != 0 {
		t.Fatal("refresh without routes must not delete anything")
	}
}

func TestRefreshReappliesRoutes(t *testing.T) {
	fixture := newCoordinatorFixture(t)
	fixture.observe(readySnapshot("en7", "172.20.10.1"))

	if err := fixture.coordinator.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !fixture.coordinator.GetStatus().RoutesApplied {
		t.Fatal("expected routes to be applied after refresh")
	}
	if fixture.manager.deletedCount() == 0 {
		t.Fatal("expected old routes to be cleared during refresh")
	}
	if starts, stops := fixture.dns.counts(); starts != 2 || stops != 1 {
		t.Fatalf("expected the DNS proxy to restart once, starts=%d stops=%d", starts, stops)
	}
}

func TestRefreshFailureLeavesRoutesUnapplied(t *testing.T) {
	fixture := newCoordinatorFixture(t)
	fixture.observe(readySnapshot("en7", "172.20.10.1"))
	fixture.manager.addErr = errors.New("route rejected")

	if err := fixture.coordinator.Refresh(); err == nil {
		t.Fatal("expected refresh to fail")
	}
	if fixture.coordinator.GetStatus().RoutesApplied {
		t.Fatal("routes must be unapplied after a failed refresh")
	}
	if starts, _ := fixture.dns.counts(); starts != 1 {
		t.Fatalf("DNS proxy must not restart after a failed refresh, starts=%d", starts)
	}
}

func TestShutdownClearsRoutesAndStopsServices(t *testing.T) {
	fixture := newCoordinatorFixture(t)
	fixture.observe(readySnapshot("en7", "172.20.10.1"))

	if err := fixture.coordinator.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if fixture.coordinator.GetStatus().RoutesApplied {
		t.Fatal("routes must be cleared on shutdown")
	}
	if fixture.manager.deletedCount() == 0 {
		t.Fatal("expected routes to be deleted on shutdown")
	}
	if fixture.dns.Running() {
		t.Fatal("DNS proxy must stop on shutdown")
	}
}
