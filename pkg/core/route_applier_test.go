package core

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newApplierFixture(t *testing.T, gateway string) (*RouteApplier, *MockRouteManager, string) {
	t.Helper()

	manager := NewMockRouteManager()
	snapshots := NewSnapshotStore()
	snapshots.Store(&NetworkSnapshot{PhoneReady: gateway != "", PhoneGateway: gateway, PhoneReason: "no phone"})
	statePath := filepath.Join(t.TempDir(), "state.yaml")

	return NewRouteApplier(manager, snapshots, statePath), manager, statePath
}

func TestNormalizeDestination(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "8.8.8.8", want: "8.8.8.8/32"},
		{input: " 1.1.1.1 ", want: "1.1.1.1/32"},
		{input: "10.0.0.0/8", want: "10.0.0.0/8"},
		{input: "192.0.2.5/24", want: "192.0.2.0/24"},
		// Private addresses stay allowed: the configuration legitimately
		// contains internal domains.
		{input: "10.233.124.212", want: "10.233.124.212/32"},
		{input: "0.0.0.0", wantErr: true},
		{input: "127.0.0.1", wantErr: true},
		{input: "224.0.0.1", wantErr: true},
		{input: "169.254.1.1", wantErr: true},
		{input: "2606:4700::1111", wantErr: true},
		{input: "not-an-ip", wantErr: true},
		{input: "", wantErr: true},
	}

	for _, testCase := range cases {
		got, err := normalizeDestination(testCase.input)
		if testCase.wantErr {
			if err == nil {
				t.Errorf("normalizeDestination(%q) should have failed, got %q", testCase.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeDestination(%q): unexpected error %v", testCase.input, err)
			continue
		}
		if got != testCase.want {
			t.Errorf("normalizeDestination(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
	}
}

func TestApplierRejectsUnusableGateway(t *testing.T) {
	for _, gateway := range []string{"", "0.0.0.0", "127.0.0.1", "garbage"} {
		manager := NewMockRouteManager()
		snapshots := NewSnapshotStore()
		snapshots.Store(&NetworkSnapshot{PhoneReady: true, PhoneGateway: gateway})
		applier := NewRouteApplier(manager, snapshots, filepath.Join(t.TempDir(), "state.yaml"))

		if err := applier.Add(RouteStatic, "203.0.113.1"); err == nil {
			t.Errorf("gateway %q should have been rejected", gateway)
		}
		if len(manager.routes()) != 0 {
			t.Errorf("gateway %q must not produce routes, got %v", gateway, manager.routes())
		}
	}
}

func TestApplierRejectsWhenPhoneIsNotReady(t *testing.T) {
	applier, manager, _ := newApplierFixture(t, "")

	if err := applier.Add(RouteStatic, "203.0.113.1"); err == nil {
		t.Fatal("expected the applier to refuse routing without a tethered path")
	}
	if len(manager.routes()) != 0 {
		t.Fatalf("expected no routes, got %v", manager.routes())
	}
}

func TestApplierIsIdempotent(t *testing.T) {
	applier, manager, _ := newApplierFixture(t, "172.20.10.1")

	for i := 0; i < 3; i++ {
		if err := applier.Add(RouteDynamic, "203.0.113.7"); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	if len(manager.routes()) != 1 {
		t.Fatalf("expected one route command, got %v", manager.routes())
	}
	if applier.Count() != 1 {
		t.Fatalf("expected one tracked route, got %d", applier.Count())
	}
}

func TestApplierTracksRoutesThatAlreadyExist(t *testing.T) {
	applier, manager, _ := newApplierFixture(t, "172.20.10.1")
	manager.addExistsFirst = true

	if err := applier.Add(RouteStatic, "203.0.113.9"); err != nil {
		t.Fatalf("an existing entry is the desired state, not a failure: %v", err)
	}
	// Tracking it is what lets cleanup remove leftovers from a previous run.
	if applier.Count() != 1 {
		t.Fatalf("expected the existing route to be tracked, got %d", applier.Count())
	}
}

func TestApplierClearRemovesRoutesAndState(t *testing.T) {
	applier, manager, statePath := newApplierFixture(t, "172.20.10.1")

	if err := applier.Add(RouteStatic, "203.0.113.1"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := applier.Add(RouteDynamic, "203.0.113.2"); err != nil {
		t.Fatalf("add: %v", err)
	}
	applier.flushState()

	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected state to be persisted: %v", err)
	}

	if err := applier.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(manager.deleted()) != 2 {
		t.Fatalf("expected two deletions, got %v", manager.deleted())
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected the state file to be removed, stat error: %v", err)
	}
}

// Host routes surviving a crash point at a gateway that may be gone, which
// blackholes the destinations the product is meant to keep reachable.
func TestApplierReconcileRemovesLeftoverRoutes(t *testing.T) {
	applier, manager, statePath := newApplierFixture(t, "172.20.10.1")

	payload := "routes:\n  203.0.113.50/32: dynamic\n  198.51.100.0/24: static\n"
	if err := os.WriteFile(statePath, []byte(payload), 0o600); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	if err := applier.ReconcileFromState(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(manager.deleted()) != 2 {
		t.Fatalf("expected both leftover routes to be deleted, got %v", manager.deleted())
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected the state file to be removed, stat error: %v", err)
	}
}

func TestApplierReconcileWithoutStateIsNotAnError(t *testing.T) {
	applier, _, _ := newApplierFixture(t, "172.20.10.1")
	if err := applier.ReconcileFromState(); err != nil {
		t.Fatalf("missing state must be tolerated: %v", err)
	}
}

// Route state is reachable from the handler goroutine the DNS library spawns per
// query, so concurrent submission must stay consistent.
func TestApplierSubmitIsSafeUnderConcurrency(t *testing.T) {
	applier, _, _ := newApplierFixture(t, "172.20.10.1")
	applier.Start()
	defer applier.Stop()

	var waitGroup sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			for i := 0; i < 20; i++ {
				applier.Submit([]string{
					"203.0.113.10",
					"203.0.113.11",
					"198.51.100.20",
				})
			}
		}(worker)
	}
	waitGroup.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && applier.Count() < 3 {
		time.Sleep(10 * time.Millisecond)
	}
	if applier.Count() != 3 {
		t.Fatalf("expected exactly three distinct routes, got %d", applier.Count())
	}
}

func TestApplierWriteStateIsAtomic(t *testing.T) {
	applier, _, statePath := newApplierFixture(t, "172.20.10.1")

	if err := applier.Add(RouteStatic, "203.0.113.1"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := applier.writeState(); err != nil {
		t.Fatalf("write state: %v", err)
	}

	state, err := loadRouteState(statePath)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state.Routes["203.0.113.1/32"] != string(RouteStatic) {
		t.Fatalf("unexpected persisted state: %#v", state.Routes)
	}

	// No temporary files must survive the rename.
	entries, err := os.ReadDir(filepath.Dir(statePath))
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(statePath) {
			t.Fatalf("unexpected leftover file %s", entry.Name())
		}
	}
}
