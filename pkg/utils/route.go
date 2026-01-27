package utils

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// AddRoute adds a static route for a network to a specific interface
func AddRoute(destination string, interfaceName string) error {
	// route add <destination> -interface <interfaceName>
	fmt.Printf("Adding route: %s via %s\n", destination, interfaceName)
	cmd := exec.Command("sudo", "route", "-n", "add", destination, "-interface", interfaceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to add route %s: %s (%v)", destination, string(output), err)
	}
	return nil
}

// AddRouteViaGateway adds a static route via a specific gateway IP
func AddRouteViaGateway(destination string, gatewayIP string) error {
	// route add <destination> <gatewayIP>
	fmt.Printf("Adding route: %s via gateway %s\n", destination, gatewayIP)
	cmd := exec.Command("sudo", "route", "-n", "add", destination, gatewayIP)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to add route %s via %s: %s (%v)", destination, gatewayIP, string(output), err)
	}
	return nil
}

// ChangeDefaultGateway changes the default route to a specific gateway IP
func ChangeDefaultGateway(gatewayIP string) error {
	// route change default <gatewayIP>
	fmt.Printf("Changing default gateway to IP: %s\n", gatewayIP)
	cmd := exec.Command("sudo", "route", "change", "default", gatewayIP)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try adding if change failed (maybe no default route exists)
		// Or maybe the current default is an interface route
		cmdAdd := exec.Command("sudo", "route", "add", "default", gatewayIP)
		outputAdd, errAdd := cmdAdd.CombinedOutput()
		if errAdd != nil {
			return fmt.Errorf("failed to change default route to %s: %s / %s", gatewayIP, string(output), string(outputAdd))
		}
	}
	return nil
}

// DeleteRoute deletes a route
func DeleteRoute(destination string) error {
	fmt.Printf("Deleting route: %s\n", destination)
	cmd := exec.Command("sudo", "route", "-n", "delete", destination)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Does not return error if route not found usually, but good to know
		return fmt.Errorf("failed to delete route %s: %s (%v)", destination, string(output), err)
	}
	return nil
}

func IsInterfaceActive(deviceName string) bool {
	// Check if interface has an IP address using ifconfig
	cmd := exec.Command("ifconfig", deviceName)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	// Very basic check for "inet "
	return strings.Contains(string(output), "inet ")
}

// ShowRoutingTable displays the current routing table using netstat
func ShowRoutingTable() error {
	fmt.Println("Current Routing Table (netstat -nr):")
	cmd := exec.Command("netstat", "-nr")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
