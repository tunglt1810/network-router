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

	return ParseHardwarePorts(string(output)), nil
}

// ParseHardwarePorts parses the output of networksetup -listallhardwareports.
func ParseHardwarePorts(output string) []InterfaceInfo {

	var interfaces []InterfaceInfo
	scanner := bufio.NewScanner(strings.NewReader(output))

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

	return interfaces
}

// FindInterfacesByName returns all hardware ports matching any keyword.
func FindInterfacesByName(interfaces []InterfaceInfo, keywords []string) []InterfaceInfo {
	var matches []InterfaceInfo
	seen := make(map[string]bool)

	for _, iface := range interfaces {
		for _, kw := range keywords {
			if kw == "" || !strings.Contains(strings.ToLower(iface.Name), strings.ToLower(kw)) {
				continue
			}

			key := iface.Name + "\x00" + iface.DeviceName
			if !seen[key] {
				matches = append(matches, iface)
				seen[key] = true
			}
			break
		}
	}

	return matches
}

// ParseIPv4Addresses extracts IPv4 addresses from ifconfig output.
func ParseIPv4Addresses(output string) []string {
	var addresses []string
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[0] != "inet" {
			continue
		}

		ip := net.ParseIP(fields[1])
		if ip != nil && ip.To4() != nil {
			addresses = append(addresses, ip.String())
		}
	}

	return addresses
}

// GetInterfaceIPv4Addresses returns the IPv4 addresses currently assigned to an interface.
func GetInterfaceIPv4Addresses(deviceName string) ([]string, error) {
	cmd := exec.Command("ifconfig", deviceName)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return ParseIPv4Addresses(string(output)), nil
}

// HasUsableIPv4 reports whether an interface has a routable IPv4 address.
func HasUsableIPv4(addresses []string) bool {
	for _, address := range addresses {
		ip := net.ParseIP(address)
		if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			continue
		}
		return true
	}

	return false
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
