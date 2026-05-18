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

// Coordinator manages the central state machine and coordinates actions
// between the NetworkDetector, Router, and DNSProxy.
type Coordinator struct {
	config        *core.Config
	router        *core.Router
	routeManager  core.RouteManager
	dnsProxy      *core.DNSProxy
	networkEvents <-chan NetworkEvent

	// Internal state protected by mutex for external readers (like IPC Status)
	mu                      sync.RWMutex
	autoRoutingEnabled      bool
	routesApplied           bool
	wifiActive              bool
	phoneActive             bool
	lastAppliedAt           time.Time
	lastClearedAt           time.Time
	dnsProxyEnabled         bool
	autoRefreshRouteEnabled bool

	refreshCron *cron.Cron
	refreshCh   chan bool
}

func NewCoordinator(
	config *core.Config,
	rm core.RouteManager,
	dnsProxy *core.DNSProxy,
	networkEvents <-chan NetworkEvent,
) *Coordinator {
	c := &Coordinator{
		config:             config,
		routeManager:       rm,
		dnsProxy:           dnsProxy,
		networkEvents:      networkEvents,
		autoRoutingEnabled: true, // Default
		refreshCh:          make(chan bool, 1),
	}
	// Initial sync from config
	c.dnsProxyEnabled = config.DNSProxyEnabled
	c.autoRefreshRouteEnabled = config.AutoRefreshRoute
	return c
}

// Start begins the event loop for the Coordinator
func (c *Coordinator) Start(ctx context.Context) error {
	log.Println("Starting State Coordinator...")

	for {
		select {
		case <-ctx.Done():
			log.Println("Coordinator stopping...")
			c.stopRefreshCron()
			return ctx.Err()
		case netEvent := <-c.networkEvents:
			c.handleNetworkEvent(netEvent)
		case <-c.refreshCh:
			c.performRefresh()
		}
	}
}

// Event Handlers

func (c *Coordinator) handleNetworkEvent(event NetworkEvent) {
	c.mu.Lock()
	c.wifiActive = event.WifiActive
	c.phoneActive = event.PhoneActive
	autoRouting := c.autoRoutingEnabled
	routesApplied := c.routesApplied
	c.mu.Unlock()

	if !autoRouting {
		// If auto-routing is disabled but routes are applied, clear them
		if routesApplied {
			log.Println("Auto-routing disabled, clearing existing routes...")
			_ = c.ForceClear()
		}
		return
	}

	bothActive := event.WifiActive && event.PhoneActive

	if bothActive && !routesApplied {
		log.Println("Both interfaces detected, applying routes...")
		if err := c.applyRoutes(); err != nil {
			log.Printf("Error applying routes: %v", err)
		} else {
			c.setRoutesApplied(true)
			log.Println("✓ Routes automatically applied")
			
			// Auto start DNS Proxy if configured
			if c.dnsProxy != nil {
				if err := c.dnsProxy.Start(); err != nil {
					log.Printf("Warning: Failed to auto-start DNS Proxy: %v", err)
				} else {
					c.setDNSProxyEnabled(true)
				}
			}
			c.startRefreshCron()
		}
	} else if !bothActive && routesApplied {
		log.Println("Interface(s) lost, clearing routes...")
		if err := c.clearRoutes(); err != nil {
			log.Printf("Error clearing routes: %v", err)
		} else {
			c.setRoutesApplied(false)
			log.Println("✓ Routes automatically cleared")
			
			// Auto stop DNS Proxy
			if c.dnsProxy != nil {
				if err := c.dnsProxy.Stop(); err != nil {
					log.Printf("Warning: Failed to auto-stop DNS Proxy: %v", err)
				} else {
					c.setDNSProxyEnabled(false)
				}
			}
			c.stopRefreshCron()
		}
	}
}

func (c *Coordinator) performRefresh() {
	log.Println("↻ Executing routing refresh sequence...")

	c.mu.RLock()
	routesApplied := c.routesApplied
	c.mu.RUnlock()

	if !routesApplied {
		log.Println("Routes are not currently applied, skipping refresh.")
		return
	}

	// 1. Stop DNS proxy
	if c.dnsProxy != nil {
		log.Println("Stopping DNS Proxy for refresh...")
		_ = c.dnsProxy.Stop()
		time.Sleep(1 * time.Second)
	}

	// 2. Clear routes
	_ = c.clearRoutes()
	c.stopRefreshCron()
	c.setRoutesApplied(false)
	c.router = nil

	// 3. Immediately re-apply since we are in refresh
	if err := c.applyRoutes(); err != nil {
		log.Printf("Error re-applying routes after refresh: %v", err)
	} else {
		c.setRoutesApplied(true)
		log.Println("✓ Routing refresh completed successfully")
		c.startRefreshCron()
		// Restart DNS Proxy
		if c.dnsProxy != nil {
			if err := c.dnsProxy.Start(); err == nil {
				c.setDNSProxyEnabled(true)
			}
		}
	}
}

// Commands from IPC

func (c *Coordinator) ForceApply() error {
	if err := c.applyRoutes(); err != nil {
		return err
	}
	c.setRoutesApplied(true)
	
	if c.dnsProxy != nil {
		if err := c.dnsProxy.Start(); err != nil {
			log.Printf("Warning: Failed to force start DNS Proxy: %v", err)
		} else {
			c.setDNSProxyEnabled(true)
		}
	}
	c.startRefreshCron()
	return nil
}

func (c *Coordinator) ForceClear() error {
	c.mu.Lock()
	autoRouting := c.autoRoutingEnabled
	if autoRouting {
		c.autoRoutingEnabled = false
		log.Println("⚠ Auto-routing disabled due to Force Clear")
	}
	c.mu.Unlock()

	if err := c.clearRoutes(); err != nil {
		return err
	}
	c.setRoutesApplied(false)

	if c.dnsProxy != nil {
		if err := c.dnsProxy.Stop(); err != nil {
			log.Printf("Warning: Failed to force stop DNS Proxy: %v", err)
		} else {
			c.setDNSProxyEnabled(false)
		}
	}
	c.stopRefreshCron()
	return nil
}

func (c *Coordinator) RefreshRoutes() {
	log.Println("↻ Queueing route refresh...")
	select {
	case c.refreshCh <- true:
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
	} else {
		if err := c.dnsProxy.Stop(); err != nil {
			return err
		}
		c.setDNSProxyEnabled(false)
	}
	return nil
}

// Internal Action Helpers

func (c *Coordinator) applyRoutes() error {
	router, err := core.NewRouter(c.config, c.routeManager)
	if err != nil {
		return err
	}
	if err := router.DetectInterfaces(); err != nil {
		return err
	}
	if err := router.ApplyRoutes(); err != nil {
		return err
	}
	c.router = router
	return nil
}

func (c *Coordinator) clearRoutes() error {
	if c.router == nil {
		router, err := core.NewRouter(c.config, c.routeManager)
		if err != nil {
			return err
		}
		c.router = router
		_ = c.router.DetectInterfaces() // Best effort
	}
	return c.router.ClearRoutes()
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
		case c.refreshCh <- true:
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

// State Accessors (used by IPC Status)

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

func (c *Coordinator) GetStatus() *RouterStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return &RouterStatus{
		AutoRoutingEnabled:      c.autoRoutingEnabled,
		RoutesApplied:           c.routesApplied,
		WifiActive:              c.wifiActive,
		PhoneActive:             c.phoneActive,
		LastAppliedAt:           c.lastAppliedAt,
		LastClearedAt:           c.lastClearedAt,
		DNSProxyEnabled:         c.dnsProxyEnabled,
		AutoRefreshRouteEnabled: c.autoRefreshRouteEnabled,
	}
}

// RouterStatus represents the current state (copied from state.go to avoid dependency issues)
type RouterStatus struct {
	AutoRoutingEnabled      bool      `json:"auto_routing_enabled"`
	RoutesApplied           bool      `json:"routes_applied"`
	WifiActive              bool      `json:"wifi_active"`
	PhoneActive             bool      `json:"phone_active"`
	LastAppliedAt           time.Time `json:"last_applied_at"`
	LastClearedAt           time.Time `json:"last_cleared_at"`
	DNSProxyEnabled         bool      `json:"dns_proxy_enabled"`
	AutoRefreshRouteEnabled bool      `json:"auto_refresh_enabled"`
}

// GetActiveRouter returns the currently active router for DNSProxy dependency
func (c *Coordinator) GetActiveRouter() *core.Router {
	return c.router
}
