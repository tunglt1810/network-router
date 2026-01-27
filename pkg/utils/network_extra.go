package utils

import (
	"fmt"
	"os/exec"
	"strings"
)

// GetInterfaceGateway retrieves the router/gateway IP for a given interface
func GetInterfaceGateway(deviceName string) (string, error) {
	// ipconfig getoption <deviceName> router
	cmd := exec.Command("ipconfig", "getoption", deviceName, "router")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	gateway := strings.TrimSpace(string(output))
	if gateway == "" {
		return "", fmt.Errorf("no gateway found for interface %s", deviceName)
	}
	return gateway, nil
}
