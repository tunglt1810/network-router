package daemon

import (
	"sync"
	"time"
)

// RouterState manages the daemon state
type RouterState struct {
	mu                 sync.RWMutex
	autoRoutingEnabled bool
	routesApplied      bool
	wifiActive         bool
	phoneActive        bool
	lastAppliedAt      time.Time
	lastClearedAt      time.Time
}

// NewRouterState creates a new RouterState
func NewRouterState() *RouterState {
	return &RouterState{
		autoRoutingEnabled: true, // Default enabled
	}
}

// IsAutoRoutingEnabled returns if auto-routing is enabled
func (s *RouterState) IsAutoRoutingEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.autoRoutingEnabled
}

// SetAutoRouting enables or disables auto-routing
func (s *RouterState) SetAutoRouting(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoRoutingEnabled = enabled
}

// AreRoutesApplied returns if routes are currently applied
func (s *RouterState) AreRoutesApplied() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.routesApplied
}

// SetRoutesApplied sets the routes applied status
func (s *RouterState) SetRoutesApplied(applied bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routesApplied = applied
	if applied {
		s.lastAppliedAt = time.Now()
	} else {
		s.lastClearedAt = time.Now()
	}
}

// UpdateInterfaceStatus updates interface active status
func (s *RouterState) UpdateInterfaceStatus(wifiActive, phoneActive bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wifiActive = wifiActive
	s.phoneActive = phoneActive
}

// GetInterfaceStatus returns current interface status
func (s *RouterState) GetInterfaceStatus() (wifiActive, phoneActive bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wifiActive, s.phoneActive
}

// HasBothInterfaces returns true if both interfaces are active
func (s *RouterState) HasBothInterfaces() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wifiActive && s.phoneActive
}

// GetStatus returns a status summary
func (s *RouterState) GetStatus() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"auto_routing_enabled": s.autoRoutingEnabled,
		"routes_applied":       s.routesApplied,
		"wifi_active":          s.wifiActive,
		"phone_active":         s.phoneActive,
		"last_applied_at":      s.lastAppliedAt,
		"last_cleared_at":      s.lastClearedAt,
	}
}
