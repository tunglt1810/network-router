package daemon

import (
	"context"
	"log"
	"network-router/pkg/core"
	"time"

	"github.com/robfig/cron/v3"
)

// Monitor watches for network interface changes
type Monitor struct {
	router        *core.Router
	state         *RouterState
	config        *core.Config
	checkInterval time.Duration
}

// NewMonitor creates a new network monitor
func NewMonitor(config *core.Config, state *RouterState) *Monitor {
	return &Monitor{
		config:        config,
		state:         state,
		checkInterval: 5 * time.Second, // Check every 5 seconds
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

	// Setup Cron for scheduled refresh
	refreshCh := make(chan bool)
	if m.config.RouteRefreshCron != "" {
		c := cron.New()
		_, err := c.AddFunc(m.config.RouteRefreshCron, func() {
			log.Println("⏰ Scheduled route refresh triggered via cron")
			refreshCh <- true
		})
		if err != nil {
			log.Printf("Error scheduling refresh cron '%s': %v", m.config.RouteRefreshCron, err)
		} else {
			c.Start()
			defer c.Stop()
			log.Printf("Route refresh scheduled with cron: %s", m.config.RouteRefreshCron)
		}
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("Network monitor stopping...")
			return ctx.Err()
		case <-ticker.C:
			if err := m.checkAndApplyRouting(); err != nil {
				log.Printf("Routing check error: %v", err)
			}
		case <-refreshCh:
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

	// 1. Clear existing routes
	if m.router != nil {
		if err := m.router.ClearRoutes(); err != nil {
			log.Printf("Warning during clear routes: %v", err)
		}
	}

	// 2. Mark as not applied to force re-application
	m.state.SetRoutesApplied(false)
	m.router = nil

	// 3. Immediately trigger re-check/apply
	if err := m.checkAndApplyRouting(); err != nil {
		log.Printf("Error re-applying routes after refresh: %v", err)
	} else {
		log.Println("✓ Routing refresh completed successfully")
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
		return nil
	}

	bothActive := wifiActive && phoneActive
	routesApplied := m.state.AreRoutesApplied()

	// Apply routes if both interfaces are active and routes not applied
	if bothActive && !routesApplied {
		log.Println("Both interfaces detected, applying routes...")
		m.router = router

		if err := m.router.ApplyRoutes(); err != nil {
			log.Printf("Error applying routes: %v", err)
			return err
		}

		m.state.SetRoutesApplied(true)
		log.Println("✓ Routes automatically applied")
	}

	// Clear routes if either interface is down and routes are applied
	if !bothActive && routesApplied {
		log.Println("Interface(s) lost, clearing routes...")

		if m.router == nil {
			m.router = router
		}

		if err := m.router.ClearRoutes(); err != nil {
			log.Printf("Error clearing routes: %v", err)
			return err
		}

		m.state.SetRoutesApplied(false)
		log.Println("✓ Routes automatically cleared")
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

	m.router = router
	m.state.SetRoutesApplied(true)

	wifiActive, phoneActive := router.GetInterfaceStatus()
	m.state.UpdateInterfaceStatus(wifiActive, phoneActive)

	return nil
}

// ForceClear forces route clearing regardless of auto-routing state
// If auto-routing is enabled, it will be automatically disabled to prevent re-application
func (m *Monitor) ForceClear() error {
	// Auto-stop routing if enabled
	if m.state.IsAutoRoutingEnabled() {
		log.Println("⚠ Auto-routing is enabled. Disabling to prevent automatic re-application...")
		m.state.SetAutoRouting(false)
		log.Println("✓ Auto-routing disabled")
	}

	if m.router == nil {
		router, err := core.NewRouter(m.config)
		if err != nil {
			return err
		}
		m.router = router
		_ = m.router.DetectInterfaces() // Best effort
	}

	if err := m.router.ClearRoutes(); err != nil {
		return err
	}

	m.state.SetRoutesApplied(false)
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
	if m.router != nil {
		if err := m.router.ClearRoutes(); err != nil {
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
	}
}
