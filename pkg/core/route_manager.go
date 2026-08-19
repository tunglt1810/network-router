package core

// RouteManager defines the interface for interacting with the OS routing table.
// This acts as a seam to allow testing routing logic without touching the host OS.
//
// Only the two operations the product actually performs are exposed. The default
// route is deliberately not part of this interface: the router installs host and
// network routes over the tethered path and must never rewrite the system default.
type RouteManager interface {
	AddRouteViaGateway(destination string, gatewayIP string) error
	DeleteRoute(destination string) error
}
