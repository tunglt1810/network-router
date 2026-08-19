package core

import "network-router/pkg/utils"

// OSRouteManager is an adapter that implements RouteManager
// by delegating to OS-specific utilities in the utils package.
type OSRouteManager struct{}

func NewOSRouteManager() *OSRouteManager {
	return &OSRouteManager{}
}

func (m *OSRouteManager) AddRouteViaGateway(destination string, gatewayIP string) error {
	return utils.AddRouteViaGateway(destination, gatewayIP)
}

func (m *OSRouteManager) DeleteRoute(destination string) error {
	return utils.DeleteRoute(destination)
}
