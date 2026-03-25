package daemon

import (
	"sync"
	"time"
)

// RouterState manages the daemon state
type RouterState struct {
	mu                      sync.RWMutex
	autoRoutingEnabled      bool
	routesApplied           bool
	wifiActive              bool
	phoneActive             bool
	lastAppliedAt           time.Time
	lastClearedAt           time.Time
	dnsProxyEnabled         bool
	autoRefreshRouteEnabled bool
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

// IsDNSProxyEnabled returns if DNS proxy is enabled
func (s *RouterState) IsDNSProxyEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dnsProxyEnabled
}

// SetDNSProxyEnabled enables or disables DNS proxy status
func (s *RouterState) SetDNSProxyEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dnsProxyEnabled = enabled
}

// IsAutoRefreshRouteEnabled returns if auto-refresh route is enabled
func (s *RouterState) IsAutoRefreshRouteEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.autoRefreshRouteEnabled
}

// SetAutoRefreshRouteEnabled enables or disables auto-refresh route
func (s *RouterState) SetAutoRefreshRouteEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoRefreshRouteEnabled = enabled
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

// RouterStatus represents the current state of the router
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

// GetStatus returns a status summary
func (s *RouterState) GetStatus() *RouterStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &RouterStatus{
		AutoRoutingEnabled:      s.autoRoutingEnabled,
		RoutesApplied:           s.routesApplied,
		WifiActive:              s.wifiActive,
		PhoneActive:             s.phoneActive,
		LastAppliedAt:           s.lastAppliedAt,
		LastClearedAt:           s.lastClearedAt,
		DNSProxyEnabled:         s.dnsProxyEnabled,
		AutoRefreshRouteEnabled: s.autoRefreshRouteEnabled,
	}
}
