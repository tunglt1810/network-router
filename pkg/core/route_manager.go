package core

// RouteManager defines the interface for interacting with the OS routing table.
// This acts as a Seam to allow testing Router logic without touching the host OS.
type RouteManager interface {
	AddRoute(destination string, interfaceName string) error
	AddRouteViaGateway(destination string, gatewayIP string) error
	ChangeDefaultGateway(gatewayIP string) error
	DeleteRoute(destination string) error
}
