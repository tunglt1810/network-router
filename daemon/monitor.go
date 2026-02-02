package daemon

import (
	"context"
	"log"
	"network-router/pkg/core"
	"os"
	"time"

	"github.com/robfig/cron/v3"
)

// Monitor watches for network interface changes
type Monitor struct {
	Router        *core.Router
	state         *RouterState
	config        *core.Config
	checkInterval time.Duration
	cron             *cron.Cron
	persistentCron   *cron.Cron
	refreshCh        chan bool
	dnsProxy         *core.DNSProxy
}

// NewMonitor creates a new network monitor
func NewMonitor(config *core.Config, state *RouterState, dnsProxy *core.DNSProxy) *Monitor {
	return &Monitor{
		config:        config,
		state:         state,
		dnsProxy:      dnsProxy,
		checkInterval: 5 * time.Second, // Check every 5 seconds
		refreshCh:     make(chan bool, 1),
	}
}

// Start begins monitoring network changes
func (m *Monitor) Start(ctx context.Context) error {
	log.Println("Starting network monitor...")

	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	// Initial check
	if err := m.checkAndApplyRouting(); err != nil {
		log.Printf("Initial routing check error: %v", err)
	}

	defer m.stopRefreshCron()
	m.startPersistentCron()
	defer m.stopPersistentCron()

	for {
		select {
		case <-ctx.Done():
			log.Println("Network monitor stopping...")
			return ctx.Err()
		case <-ticker.C:
			if err := m.checkAndApplyRouting(); err != nil {
				log.Printf("Routing check error: %v", err)
			}
		case <-m.refreshCh:
			m.performRefresh()
		}
	}
}

// performRefresh handles the forced refresh of routing rules
func (m *Monitor) performRefresh() {
	log.Println("↻ Executing routing refresh sequence...")

	// Only refresh if currently applied
	if !m.state.AreRoutesApplied() {
		log.Println("Routes are not currently applied, skipping refresh.")
		return
	}

	// 1. Clear existing routes and stop cron
	if m.Router != nil {
		if err := m.Router.ClearRoutes(); err != nil {
			log.Printf("Warning during clear routes: %v", err)
		}
	}
	m.stopRefreshCron()

	// 2. Mark as not applied to force re-application
	m.state.SetRoutesApplied(false)
	m.Router = nil

	// 3. Immediately trigger re-check/apply
	if err := m.checkAndApplyRouting(); err != nil {
		log.Printf("Error re-applying routes after refresh: %v", err)
	} else {
		log.Println("✓ Routing refresh completed successfully")
		// startRefreshCron is called inside checkAndApplyRouting if it succeeds
	}
}

// checkAndApplyRouting checks interface status and applies/clears routing accordingly
func (m *Monitor) checkAndApplyRouting() error {
	// Create new router for detection
	router, err := core.NewRouter(m.config)
	if err != nil {
		return err
	}

	// Try to detect interfaces
	err = router.DetectInterfaces()

	wifiActive, phoneActive := false, false
	if err == nil {
		wifiActive, phoneActive = router.GetInterfaceStatus()
	}

	// Update state
	m.state.UpdateInterfaceStatus(wifiActive, phoneActive)

	// Check if we should take action
	if !m.state.IsAutoRoutingEnabled() {
		// If auto-routing is disabled but routes are currently applied, we must clear them
		if m.state.AreRoutesApplied() {
			log.Println("Auto-routing disabled, clearing existing routes...")
			if err := m.ForceClear(); err != nil {
				log.Printf("Error clearing routes after disable: %v", err)
			}
		}
		return nil
	}

	bothActive := wifiActive && phoneActive
	routesApplied := m.state.AreRoutesApplied()

	// Apply routes if both interfaces are active and routes not applied
	if bothActive && !routesApplied {
		log.Println("Both interfaces detected, applying routes...")
		m.Router = router

		if err := m.Router.ApplyRoutes(); err != nil {
			log.Printf("Error applying routes: %v", err)
			return err
		}

		m.state.SetRoutesApplied(true)
		log.Println("✓ Routes automatically applied")
		
		// Start DNS Proxy if enabled
		if m.dnsProxy != nil {
			if err := m.dnsProxy.Start(); err != nil {
				log.Printf("Warning: Failed to auto-start DNS Proxy: %v", err)
			} else {
				m.state.SetDNSProxyEnabled(true)
			}
		}
		
		m.startRefreshCron()
	}

	// Clear routes if either interface is down and routes are applied
	if !bothActive && routesApplied {
		log.Println("Interface(s) lost, clearing routes...")

		if m.Router == nil {
			m.Router = router
		}

		if err := m.Router.ClearRoutes(); err != nil {
			log.Printf("Error clearing routes: %v", err)
			return err
		}

		m.state.SetRoutesApplied(false)
		log.Println("✓ Routes automatically cleared")
		
		// Stop DNS Proxy
		if m.dnsProxy != nil {
			if err := m.dnsProxy.Stop(); err != nil {
				log.Printf("Warning: Failed to auto-stop DNS Proxy: %v", err)
			} else {
				m.state.SetDNSProxyEnabled(false)
			}
		}
		
		m.stopRefreshCron()
	}

	return nil
}

// ForceApply forces route application regardless of auto-routing state
func (m *Monitor) ForceApply() error {
	router, err := core.NewRouter(m.config)
	if err != nil {
		return err
	}

	if err := router.DetectInterfaces(); err != nil {
		return err
	}

	if err := router.ApplyRoutes(); err != nil {
		return err
	}

	m.Router = router
	m.state.SetRoutesApplied(true)
	
	// Start DNS Proxy
	if m.dnsProxy != nil {
		if err := m.dnsProxy.Start(); err != nil {
			log.Printf("Warning: Failed to force start DNS Proxy: %v", err)
		} else {
			m.state.SetDNSProxyEnabled(true)
		}
	}
	
	m.startRefreshCron()

	wifiActive, phoneActive := router.GetInterfaceStatus()
	m.state.UpdateInterfaceStatus(wifiActive, phoneActive)

	return nil
}

// ForceClear forces route clearing regardless of auto-routing state
// If auto-routing is enabled, it will be automatically disabled to prevent re-application
func (m *Monitor) ForceClear() error {
	// Auto-stop routing if enabled
	if m.state.IsAutoRoutingEnabled() {
		log.Println("✓ Auto-routing is enabled. Disabling to prevent automatic re-application...")
		m.state.SetAutoRouting(false)
		log.Println("⚠ Auto-routing disabled")
	}

	if m.Router == nil {
		router, err := core.NewRouter(m.config)
		if err != nil {
			return err
		}
		m.Router = router
		_ = m.Router.DetectInterfaces() // Best effort
	}

	if err := m.Router.ClearRoutes(); err != nil {
		return err
	}

	m.state.SetRoutesApplied(false)
	
	// Stop DNS Proxy
	if m.dnsProxy != nil {
		if err := m.dnsProxy.Stop(); err != nil {
			log.Printf("Warning: Failed to force stop DNS Proxy: %v", err)
		} else {
			m.state.SetDNSProxyEnabled(false)
		}
	}
	
	m.stopRefreshCron()
	return nil
}

// RefreshRoutes performs a scheduled refresh of routing rules
func (m *Monitor) RefreshRoutes() {
	log.Println("↻ Scheduled Refresh: Recalculating routes...")

	// We only refresh if routes are currently applied
	if !m.state.AreRoutesApplied() {
		log.Println("Skipping refresh: Routes are not currently applied.")
		return
	}

	// 1. Clear existing routes to remove stale IPs
	log.Println("stop current routing...")
	if m.Router != nil {
		if err := m.Router.ClearRoutes(); err != nil {
			log.Printf("Warning during refresh cleanup: %v", err)
		}
	}

	// Reset state so checkAndApplyRouting can re-apply
	m.state.SetRoutesApplied(false)

	// 2. Re-apply routes (this will resolve DNS again)
	log.Println("re-applying routing...")
	if err := m.checkAndApplyRouting(); err != nil {
		log.Printf("Error during route refresh application: %v", err)
	} else {
		log.Println("✓ Routes refreshed successfully")
		m.startRefreshCron()
	}
}

// startRefreshCron starts the scheduled refresh cronjob if configured
func (m *Monitor) startRefreshCron() {
	if m.config.RouteRefreshCron == "" {
		return
	}

	if m.cron != nil {
		return // Already running
	}

	c := cron.New()
	_, err := c.AddFunc(m.config.RouteRefreshCron, func() {
		log.Println("⏰ Scheduled route refresh triggered via cron")
		select {
		case m.refreshCh <- true:
		default:
			// Channel full, skip trigger
		}
	})

	if err != nil {
		log.Printf("Error scheduling refresh cron '%s': %v", m.config.RouteRefreshCron, err)
		return
	}

	m.cron = c
	c.Start()
	log.Printf("Route refresh scheduled with cron: %s", m.config.RouteRefreshCron)
}

// stopRefreshCron stops the scheduled refresh cronjob
func (m *Monitor) stopRefreshCron() {
	if m.cron != nil {
		log.Println("Stopping scheduled refresh cronjob...")
		m.cron.Stop()
		m.cron = nil
	}
}

// startPersistentCron starts the persistent background tasks
func (m *Monitor) startPersistentCron() {
	if m.persistentCron != nil {
		return
	}

	m.persistentCron = cron.New()

	// Clear log file at 10:00 AM every day
	_, err := m.persistentCron.AddFunc("0 10 * * *", func() {
		log.Println("⏰ Scheduled log cleanup: Truncating /var/log/network-router.log")
		if err := os.Truncate("/var/log/network-router.log", 0); err != nil {
			log.Printf("Error truncating log file: %v", err)
		}
	})

	if err != nil {
		log.Printf("Error scheduling persistent cron: %v", err)
		return
	}

	m.persistentCron.Start()
	log.Println("Persistent cronjob started (Log cleanup scheduled at 10:00 AM)")
}

// stopPersistentCron stops the persistent background tasks
func (m *Monitor) stopPersistentCron() {
	if m.persistentCron != nil {
		log.Println("Stopping persistent cronjob...")
		m.persistentCron.Stop()
		m.persistentCron = nil
	}
}
