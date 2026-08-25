package daemon

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"network-router/pkg/core"

	"github.com/robfig/cron/v3"
)

// DNSController is the lifecycle seam used by the coordinator.
type DNSController interface {
	Start() error
	Stop() error
}

// Coordinator manages the central state machine and coordinates actions between
// the NetworkDetector, Router, and DNSProxy.
//
// Network facts are not duplicated here. They are read from the shared
// SnapshotStore, so status reporting cannot drift from what routing decisions
// actually used.
type Coordinator struct {
	config    *core.Config
	router    *core.Router
	applier   *core.RouteApplier
	dnsProxy  DNSController
	snapshots *core.SnapshotStore
	events    <-chan *core.NetworkSnapshot

	mu                      sync.RWMutex
	autoRoutingEnabled      bool
	routesApplied           bool
	dnsProxyEnabled         bool
	autoRefreshRouteEnabled bool
	lastPhoneDevice         string
	lastPhoneGateway        string
	lastAppliedAt           time.Time
	lastClearedAt           time.Time

	refreshCron *cron.Cron
	refreshCh   chan struct{}
}

func NewCoordinator(
	config *core.Config,
	router *core.Router,
	applier *core.RouteApplier,
	dnsProxy DNSController,
	snapshots *core.SnapshotStore,
	events <-chan *core.NetworkSnapshot,
) *Coordinator {
	if snapshots == nil {
		snapshots = core.NewSnapshotStore()
	}
	return &Coordinator{
		config:                  config,
		router:                  router,
		applier:                 applier,
		dnsProxy:                dnsProxy,
		snapshots:               snapshots,
		events:                  events,
		autoRoutingEnabled:      true,
		autoRefreshRouteEnabled: config.AutoRefreshRoute,
		refreshCh:               make(chan struct{}, 1),
	}
}

// Start begins the event loop for the Coordinator.
func (c *Coordinator) Start(ctx context.Context) error {
	log.Println("Starting State Coordinator...")

	for {
		select {
		case <-ctx.Done():
			log.Println("Coordinator stopping...")
			c.stopRefreshCron()
			return ctx.Err()
		case snapshot := <-c.events:
			c.handleSnapshot(snapshot)
		case <-c.refreshCh:
			c.performRefresh()
		}
	}
}

func (c *Coordinator) handleSnapshot(snapshot *core.NetworkSnapshot) {
	if snapshot == nil {
		return
	}

	c.mu.Lock()
	autoRouting := c.autoRoutingEnabled
	routesApplied := c.routesApplied
	pathChanged := routesApplied &&
		((snapshot.PhoneDevice() != "" && snapshot.PhoneDevice() != c.lastPhoneDevice) ||
			(snapshot.PhoneGateway != "" && snapshot.PhoneGateway != c.lastPhoneGateway))
	c.lastPhoneDevice = snapshot.PhoneDevice()
	c.lastPhoneGateway = snapshot.PhoneGateway
	c.mu.Unlock()

	if !autoRouting {
		if routesApplied {
			log.Println("Auto-routing disabled, clearing existing routes...")
			_ = c.ForceClear()
		}
		return
	}

	usable := snapshot.WifiActive && snapshot.PhoneReady

	switch {
	case usable && (!routesApplied || pathChanged):
		if routesApplied {
			log.Println("Phone routing path changed, refreshing routes...")
			c.stopManagedServices()
			if err := c.applier.Clear(); err != nil {
				log.Printf("Error clearing routes before rebind: %v", err)
			}
			c.setRoutesApplied(false)
		}

		log.Println("Tethered path available, applying routes...")
		if err := c.router.ApplyRoutes(); err != nil {
			log.Printf("Error applying routes: %v", err)
			return
		}
		c.setRoutesApplied(true)
		log.Println("✓ Routes automatically applied")
		c.startManagedServices()

	case !usable && routesApplied:
		log.Printf("Tethered path lost (%s), clearing routes...", snapshot.PhoneReason)
		c.stopManagedServices()
		if err := c.applier.Clear(); err != nil {
			log.Printf("Error clearing routes: %v", err)
		}
		c.setRoutesApplied(false)
		log.Println("✓ Routes automatically cleared")
	}
}

func (c *Coordinator) startManagedServices() {
	if c.dnsProxy != nil {
		if err := c.dnsProxy.Start(); err != nil {
			log.Printf("Warning: Failed to start DNS Proxy: %v", err)
			c.setDNSProxyEnabled(false)
		} else {
			c.setDNSProxyEnabled(true)
		}
	}
	c.startRefreshCron()
}

func (c *Coordinator) stopManagedServices() {
	if c.dnsProxy != nil {
		if err := c.dnsProxy.Stop(); err != nil {
			log.Printf("Warning: Failed to stop DNS Proxy: %v", err)
		}
		c.setDNSProxyEnabled(false)
	}
	c.stopRefreshCron()
}

func (c *Coordinator) performRefresh() {
	if err := c.ForceRefresh(); err != nil {
		log.Printf("Refresh error: %v", err)
	}
}

// Commands from IPC

func (c *Coordinator) ForceApply() error {
	if err := c.router.ApplyRoutes(); err != nil {
		return err
	}
	c.setRoutesApplied(true)
	c.startManagedServices()
	return nil
}

func (c *Coordinator) ForceClear() error {
	c.mu.Lock()
	if c.autoRoutingEnabled {
		c.autoRoutingEnabled = false
		log.Println("⚠ Auto-routing disabled due to Force Clear")
	}
	c.mu.Unlock()

	c.stopManagedServices()
	if err := c.applier.Clear(); err != nil {
		return err
	}
	c.setRoutesApplied(false)
	return nil
}

func (c *Coordinator) ForceRefresh() error {
	c.mu.RLock()
	routesApplied := c.routesApplied
	c.mu.RUnlock()

	if !routesApplied {
		return fmt.Errorf("routes are not currently applied, skipping refresh")
	}

	log.Println("↻ Executing routing refresh sequence...")
	c.stopManagedServices()
	if err := c.applier.Clear(); err != nil {
		log.Printf("Error clearing routes during refresh: %v", err)
	}
	c.setRoutesApplied(false)

	if err := c.router.ApplyRoutes(); err != nil {
		log.Printf("Error re-applying routes after refresh: %v", err)
		return err
	}
	c.setRoutesApplied(true)
	c.startManagedServices()
	log.Println("✓ Routing refresh completed successfully")
	return nil
}

func (c *Coordinator) RefreshRoutes() {
	log.Println("↻ Queueing route refresh...")
	select {
	case c.refreshCh <- struct{}{}:
	default:
		log.Println("Refresh already queued/running, skipping duplicate request")
	}
}

func (c *Coordinator) SetAutoRouting(enabled bool) {
	c.mu.Lock()
	c.autoRoutingEnabled = enabled
	c.mu.Unlock()
}

func (c *Coordinator) SetAutoRefresh(enabled bool) {
	c.mu.Lock()
	c.autoRefreshRouteEnabled = enabled
	c.mu.Unlock()
	if enabled {
		c.startRefreshCron()
	} else {
		c.stopRefreshCron()
	}
}

func (c *Coordinator) SetDNSProxy(enabled bool) error {
	if c.dnsProxy == nil {
		return fmt.Errorf("DNS Proxy not initialized")
	}

	if enabled {
		if err := c.dnsProxy.Start(); err != nil {
			return err
		}
		c.setDNSProxyEnabled(true)
		return nil
	}

	if err := c.dnsProxy.Stop(); err != nil {
		return err
	}
	c.setDNSProxyEnabled(false)
	return nil
}

func (c *Coordinator) startRefreshCron() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.config.RouteRefreshCron == "" || !c.autoRefreshRouteEnabled || c.refreshCron != nil {
		return
	}

	cronJob := cron.New()
	_, err := cronJob.AddFunc(c.config.RouteRefreshCron, func() {
		log.Println("⏰ Scheduled route refresh triggered via cron")
		select {
		case c.refreshCh <- struct{}{}:
		default:
		}
	})
	if err != nil {
		log.Printf("Error scheduling refresh cron: %v", err)
		return
	}

	c.refreshCron = cronJob
	cronJob.Start()
	log.Printf("Route refresh scheduled with cron: %s", c.config.RouteRefreshCron)
}

func (c *Coordinator) stopRefreshCron() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refreshCron != nil {
		log.Println("Stopping scheduled refresh cronjob...")
		c.refreshCron.Stop()
		c.refreshCron = nil
	}
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

func (c *Coordinator) setDNSProxyEnabled(enabled bool) {
	c.mu.Lock()
	c.dnsProxyEnabled = enabled
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
		AutoRoutingEnabled:      c.autoRoutingEnabled,
		RoutesApplied:           c.routesApplied,
		WifiActive:              snapshot.WifiActive,
		PhoneActive:             snapshot.PhoneReady,
		WifiDevice:              snapshot.WifiDevice(),
		PhoneDevice:             snapshot.PhoneDevice(),
		WifiGateway:             snapshot.WifiGateway,
		PhoneGateway:            snapshot.PhoneGateway,
		PhoneReady:              snapshot.PhoneReady,
		PhoneStatusReason:       snapshot.PhoneReason,
		ActiveRoutes:            c.applier.Count(),
		LastAppliedAt:           c.lastAppliedAt,
		LastClearedAt:           c.lastClearedAt,
		DNSProxyEnabled:         c.dnsProxyEnabled,
		AutoRefreshRouteEnabled: c.autoRefreshRouteEnabled,
	}
}

// RouterStatus represents the current state reported over IPC.
type RouterStatus struct {
	AutoRoutingEnabled      bool      `json:"auto_routing_enabled"`
	RoutesApplied           bool      `json:"routes_applied"`
	WifiActive              bool      `json:"wifi_active"`
	PhoneActive             bool      `json:"phone_active"`
	WifiDevice              string    `json:"wifi_device,omitempty"`
	PhoneDevice             string    `json:"phone_device,omitempty"`
	WifiGateway             string    `json:"wifi_gateway,omitempty"`
	PhoneGateway            string    `json:"phone_gateway,omitempty"`
	PhoneReady              bool      `json:"phone_ready"`
	PhoneStatusReason       string    `json:"phone_status_reason,omitempty"`
	ActiveRoutes            int       `json:"active_routes"`
	LastAppliedAt           time.Time `json:"last_applied_at"`
	LastClearedAt           time.Time `json:"last_cleared_at"`
	DNSProxyEnabled         bool      `json:"dns_proxy_enabled"`
	AutoRefreshRouteEnabled bool      `json:"auto_refresh_enabled"`
}
