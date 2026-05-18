package core

import (
	"testing"
	"network-router/pkg/utils"
)

type MockRouteManager struct {
	addedRoutes    []string
	deletedRoutes  []string
	defaultGateway string
}

func NewMockRouteManager() *MockRouteManager {
	return &MockRouteManager{
		addedRoutes:   make([]string, 0),
		deletedRoutes: make([]string, 0),
	}
}

func (m *MockRouteManager) AddRoute(destination string, interfaceName string) error {
	m.addedRoutes = append(m.addedRoutes, destination)
	return nil
}

func (m *MockRouteManager) AddRouteViaGateway(destination string, gatewayIP string) error {
	m.addedRoutes = append(m.addedRoutes, destination)
	return nil
}

func (m *MockRouteManager) ChangeDefaultGateway(gatewayIP string) error {
	m.defaultGateway = gatewayIP
	return nil
}

func (m *MockRouteManager) DeleteRoute(destination string) error {
	m.deletedRoutes = append(m.deletedRoutes, destination)
	return nil
}

func TestRouterApplyRoutes(t *testing.T) {
	config := &Config{
		TetherCIDRs: []string{"192.168.100.0/24"},
	}
	mockRM := NewMockRouteManager()
	router, err := NewRouter(config, mockRM)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	// Mock interfaces
	router.wifiIface = &utils.InterfaceInfo{DeviceName: "en0"}
	router.phoneIface = &utils.InterfaceInfo{DeviceName: "en1"}
	router.resolvedIPs = []string{"8.8.8.8"}

	// Note: ApplyRoutes calls ResolveDomains, which uses OS. 
	// To properly unit test ApplyRoutes we would also need to mock DNS and Gateway fetching.
	// But this tests the interaction with RouteManager for ClearRoutes
	
	err = router.ClearRoutes()
	if err != nil {
		t.Fatalf("ClearRoutes failed: %v", err)
	}
	
	// We should see routes being deleted
	if len(mockRM.deletedRoutes) == 0 {
		t.Errorf("Expected routes to be deleted, got 0")
	}
}
