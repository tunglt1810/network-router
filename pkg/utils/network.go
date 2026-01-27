package utils

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

// InterfaceInfo holds information about a network interface
type InterfaceInfo struct {
	Name       string // e.g., "Wi-Fi"
	DeviceName string // e.g., "en0"
	MacAddress string
}

// GetNetworkInterfaces processes 'networksetup -listallhardwareports' to find interfaces
func GetNetworkInterfaces() ([]InterfaceInfo, error) {
	cmd := exec.Command("networksetup", "-listallhardwareports")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var interfaces []InterfaceInfo
	scanner := bufio.NewScanner(strings.NewReader(string(output)))

	var currentInterface InterfaceInfo

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Hardware Port:") {
			if currentInterface.Name != "" {
				interfaces = append(interfaces, currentInterface)
			}
			currentInterface = InterfaceInfo{
				Name: strings.TrimSpace(strings.TrimPrefix(line, "Hardware Port:")),
			}
		} else if strings.HasPrefix(line, "Device:") {
			currentInterface.DeviceName = strings.TrimSpace(strings.TrimPrefix(line, "Device:"))
		} else if strings.HasPrefix(line, "Ethernet Address:") {
			currentInterface.MacAddress = strings.TrimSpace(strings.TrimPrefix(line, "Ethernet Address:"))
		}
	}
	// Append the last one
	if currentInterface.Name != "" {
		interfaces = append(interfaces, currentInterface)
	}

	return interfaces, nil
}

// FindInterfaceByName searches for a specific interface by common keywords
func FindInterfaceByName(interfaces []InterfaceInfo, keywords []string) *InterfaceInfo {
	for _, iface := range interfaces {
		for _, kw := range keywords {
			if strings.Contains(strings.ToLower(iface.Name), strings.ToLower(kw)) {
				return &iface
			}
		}
	}
	return nil
}

// ResolveDomainToIPs resolves a domain name to a list of IPs
func ResolveDomainToIPs(domain string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", domain)
	if err != nil {
		return nil, fmt.Errorf("DNS resolution failed: %v", err)
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no IPv4 addresses found")
	}

	var ipStrings []string
	for _, ip := range ips {
		if ip.To4() != nil {
			ipStrings = append(ipStrings, ip.String())
		}
	}

	if len(ipStrings) == 0 {
		return nil, fmt.Errorf("no IPv4 addresses found")
	}

	return ipStrings, nil
}
