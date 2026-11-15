package daemon

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"network-router/pkg/core"
)

// DNSController is the lifecycle seam used by the coordinator.
type DNSController interface {
	Start() error
	Stop() error
	Running() bool
}

// Coordinator manages the central state machine and coordinates actions between
// the NetworkDetector, Router, and DNSProxy.
//
// Network facts are not duplicated here. They are read from the shared
// SnapshotStore, so status reporting cannot drift from what routing decisions
// actually used.
type Coordinator struct {
	router    *core.Router
	applier   *core.RouteApplier
	dnsProxy  DNSController
	snapshots *core.SnapshotStore
	events    <-chan *core.NetworkSnapshot

	// opMu serializes route operations (IPC commands and network events), so
	// apply and clear never interleave. mu only guards the fields below.
	opMu sync.Mutex

	mu               sync.RWMutex
	enabled          bool
	routesApplied    bool
	lastPhoneDevice  string
	lastPhoneGateway string
	lastAppliedAt    time.Time
	lastClearedAt    time.Time
}

func NewCoordinator(
	router *core.Router,
	applier *core.RouteApplier,
	dnsProxy DNSController,
	snapshots *core.SnapshotStore,
	events <-chan *core.NetworkSnapshot,
) *Coordinator {
	return &Coordinator{
		router:    router,
		applier:   applier,
		dnsProxy:  dnsProxy,
		snapshots: snapshots,
		events:    events,
		enabled:   true,
	}
}

// Start begins the event loop for the Coordinator.
func (c *Coordinator) Start(ctx context.Context) error {
	log.Println("Starting State Coordinator...")

	for {
		select {
		case <-ctx.Done():
			log.Println("Coordinator stopping...")
			return ctx.Err()
		case snapshot := <-c.events:
			c.handleSnapshot(snapshot)
		}
	}
}

func (c *Coordinator) handleSnapshot(snapshot *core.NetworkSnapshot) {
	if snapshot == nil {
		return
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := c.reconcileLocked(snapshot); err != nil {
		log.Printf("Error applying routes: %v", err)
	}
}

// reconcileLocked brings the routes in line with the snapshot and the on/off
// switch. It is the only path that applies routes, so the recorded phone path
// always matches the path the routes were bound to. The caller must hold opMu.
func (c *Coordinator) reconcileLocked(snapshot *core.NetworkSnapshot) error {
	c.mu.Lock()
	enabled := c.enabled
	routesApplied := c.routesApplied
	pathChanged := routesApplied &&
		((snapshot.PhoneDevice() != "" && snapshot.PhoneDevice() != c.lastPhoneDevice) ||
			(snapshot.PhoneGateway != "" && snapshot.PhoneGateway != c.lastPhoneGateway))
	c.lastPhoneDevice = snapshot.PhoneDevice()
	c.lastPhoneGateway = snapshot.PhoneGateway
	c.mu.Unlock()

	usable := enabled && snapshot.PhoneReady

	switch {
	case usable && (!routesApplied || pathChanged):
		if routesApplied {
			log.Println("Phone routing path changed, refreshing routes...")
			if err := c.clearRoutesLocked(); err != nil {
				log.Printf("Error clearing routes before rebind: %v", err)
			}
		}
		log.Println("Tethered path available, applying routes...")
		if err := c.applyRoutesLocked(snapshot); err != nil {
			return err
		}
		log.Println("✓ Routes applied")

	case !usable && routesApplied:
		if enabled {
			log.Printf("Tethered path lost (%s), clearing routes...", snapshot.PhoneReason)
		} else {
			log.Println("Routing is off, clearing routes...")
		}
		if err := c.clearRoutesLocked(); err != nil {
			return err
		}
		log.Println("✓ Routes cleared")
	}
	return nil
}

// applyRoutesLocked installs routes, then starts the DNS proxy. The caller
// must hold opMu.
func (c *Coordinator) applyRoutesLocked(snapshot *core.NetworkSnapshot) error {
	if err := c.router.ApplyRoutes(snapshot); err != nil {
		return err
	}
	c.setRoutesApplied(true)
	if err := c.dnsProxy.Start(); err != nil {
		log.Printf("Warning: Failed to start DNS Proxy: %v", err)
	}
	return nil
}

// clearRoutesLocked stops the DNS proxy, then removes routes. Routes are
// marked not applied even when clearing reports an error. The caller must hold
// opMu.
func (c *Coordinator) clearRoutesLocked() error {
	if err := c.dnsProxy.Stop(); err != nil {
		log.Printf("Warning: Failed to stop DNS Proxy: %v", err)
	}
	err := c.applier.Clear()
	c.setRoutesApplied(false)
	return err
}

// On turns routing on and applies routes when the tethered path is usable.
// When it is not, it returns applied=false, and the next snapshot applies
// the routes once the path becomes ready.
func (c *Coordinator) On() (applied bool, err error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()

	c.mu.Lock()
	c.enabled = true
	c.mu.Unlock()

	snapshot := c.snapshots.Load()
	if snapshot == nil {
		snapshot = &core.NetworkSnapshot{PhoneReason: "network has not been discovered yet"}
	}
	if err := c.reconcileLocked(snapshot); err != nil {
		return false, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.routesApplied {
		log.Printf("Routing is on: waiting for tethering path (%s)", snapshot.PhoneReason)
	}
	return c.routesApplied, nil
}

// Off turns routing off and removes routes. Routing stays off until On.
func (c *Coordinator) Off() error {
	c.opMu.Lock()
	defer c.opMu.Unlock()

	c.mu.Lock()
	c.enabled = false
	c.mu.Unlock()
	return c.clearRoutesLocked()
}

// Shutdown stops the DNS proxy and removes routes. It waits for any running
// operation, so no apply can add routes after the clear.
func (c *Coordinator) Shutdown() error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.clearRoutesLocked()
}

// Refresh removes the routes and applies them again, which resolves the
// configured domains again.
func (c *Coordinator) Refresh() error {
	c.opMu.Lock()
	defer c.opMu.Unlock()

	c.mu.RLock()
	routesApplied := c.routesApplied
	c.mu.RUnlock()

	if !routesApplied {
		return fmt.Errorf("routes are not currently applied, skipping refresh")
	}

	log.Println("↻ Executing routing refresh sequence...")
	if err := c.clearRoutesLocked(); err != nil {
		log.Printf("Error clearing routes during refresh: %v", err)
	}

	if err := c.applyRoutesLocked(c.snapshots.Load()); err != nil {
		log.Printf("Error re-applying routes after refresh: %v", err)
		return err
	}
	log.Println("✓ Routing refresh completed successfully")
	return nil
}

func (c *Coordinator) setRoutesApplied(applied bool) {
	c.mu.Lock()
	c.routesApplied = applied
	if applied {
		c.lastAppliedAt = time.Now()
	} else {
		c.lastClearedAt = time.Now()
	}
	c.mu.Unlock()
}

// GetStatus builds the reported status from the published snapshot plus the
// lifecycle flags this coordinator owns.
func (c *Coordinator) GetStatus() *RouterStatus {
	snapshot := c.snapshots.Load()
	if snapshot == nil {
		snapshot = &core.NetworkSnapshot{}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return &RouterStatus{
		AutoRoutingEnabled: c.enabled,
		RoutesApplied:      c.routesApplied,
		PhoneDevice:        snapshot.PhoneDevice(),
		PhoneGateway:       snapshot.PhoneGateway,
		PhoneReady:         snapshot.PhoneReady,
		PhoneStatusReason:  snapshot.PhoneReason,
		ActiveRoutes:       c.applier.Count(),
		LastAppliedAt:      c.lastAppliedAt,
		LastClearedAt:      c.lastClearedAt,
		DNSProxyRunning:    c.dnsProxy.Running(),
	}
}

// RouterStatus represents the current state reported over IPC.
type RouterStatus struct {
	AutoRoutingEnabled bool      `json:"auto_routing_enabled"`
	RoutesApplied      bool      `json:"routes_applied"`
	PhoneDevice        string    `json:"phone_device,omitempty"`
	PhoneGateway       string    `json:"phone_gateway,omitempty"`
	PhoneReady         bool      `json:"phone_ready"`
	PhoneStatusReason  string    `json:"phone_status_reason,omitempty"`
	ActiveRoutes       int       `json:"active_routes"`
	LastAppliedAt      time.Time `json:"last_applied_at"`
	LastClearedAt      time.Time `json:"last_cleared_at"`
	DNSProxyRunning    bool      `json:"dns_proxy_running"`
}
