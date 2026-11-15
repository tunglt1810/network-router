package utils

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrRouteExists reports that the destination is already present in the routing
// table. Adding a route is idempotent from the caller's point of view, so this
// is a benign outcome rather than a failure.
var ErrRouteExists = errors.New("route already exists")

// ErrRouteMissing reports that the destination was not in the routing table
// when a delete was attempted. Deleting is idempotent, so this is benign too.
var ErrRouteMissing = errors.New("route not in table")

// AddRouteViaGateway adds a static route via a specific gateway IP.
// The daemon already runs as root, so `route` is invoked directly.
func (System) AddRouteViaGateway(destination string, gatewayIP string) error {
	output, err := exec.Command("route", "-n", "add", destination, gatewayIP).CombinedOutput()
	if err == nil {
		return nil
	}
	if strings.Contains(string(output), "File exists") {
		return ErrRouteExists
	}
	return fmt.Errorf("add route %s via %s: %s (%w)", destination, gatewayIP, strings.TrimSpace(string(output)), err)
}

// DeleteRoute removes a route from the routing table.
func (System) DeleteRoute(destination string) error {
	output, err := exec.Command("route", "-n", "delete", destination).CombinedOutput()
	if err == nil {
		return nil
	}
	text := string(output)
	if strings.Contains(text, "not in table") || strings.Contains(text, "No such process") {
		return ErrRouteMissing
	}
	return fmt.Errorf("delete route %s: %s (%w)", destination, strings.TrimSpace(text), err)
}
