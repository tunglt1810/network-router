package utils

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
)

// GetInterfaceGateway retrieves the router/gateway IP for a given interface.
// A gateway that cannot be used for routing (unparseable, unspecified such as
// 0.0.0.0, loopback, or multicast) is rejected rather than returned, because
// installing routes through it would silently blackhole traffic.
func (System) GetInterfaceGateway(deviceName string) (string, error) {
	output, err := exec.Command("ipconfig", "getoption", deviceName, "router").Output()
	if err != nil {
		return "", err
	}

	gateway := strings.TrimSpace(string(output))
	if gateway == "" {
		return "", fmt.Errorf("no gateway found for interface %s", deviceName)
	}
	if err := ValidateGateway(gateway); err != nil {
		return "", fmt.Errorf("interface %s: %w", deviceName, err)
	}
	return gateway, nil
}

// ValidateGateway reports whether an address is usable as a next hop.
func ValidateGateway(gateway string) error {
	ip := net.ParseIP(strings.TrimSpace(gateway))
	if ip == nil {
		return fmt.Errorf("gateway %q is not a valid IP address", gateway)
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("gateway %q is unspecified", gateway)
	}
	if ip.IsLoopback() {
		return fmt.Errorf("gateway %q is a loopback address", gateway)
	}
	if ip.IsMulticast() {
		return fmt.Errorf("gateway %q is a multicast address", gateway)
	}
	return nil
}

var dhcpDNSPattern = regexp.MustCompile(`domain_name_server[^{]*\{([^}]*)\}`)

// GetInterfaceDNSServers returns the DNS servers advertised by DHCP on an
// interface. This is how the proxy finds the resolver reachable over the
// tethered path instead of the resolver of the network being bypassed.
func GetInterfaceDNSServers(deviceName string) []string {
	if servers := parseDHCPDNS(runIPConfig("getpacket", deviceName)); len(servers) > 0 {
		return servers
	}

	// Older DHCP responses only expose the option directly.
	output := strings.TrimSpace(runIPConfig("getoption", deviceName, "domain_name_server"))
	return filterUsableDNS(strings.Fields(output))
}

func runIPConfig(args ...string) string {
	output, err := exec.Command("ipconfig", args...).Output()
	if err != nil {
		return ""
	}
	return string(output)
}

func parseDHCPDNS(packet string) []string {
	match := dhcpDNSPattern.FindStringSubmatch(packet)
	if len(match) < 2 {
		return nil
	}
	return filterUsableDNS(strings.FieldsFunc(match[1], func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}))
}

func filterUsableDNS(candidates []string) []string {
	var servers []string
	seen := make(map[string]bool)

	for _, candidate := range candidates {
		ip := net.ParseIP(strings.TrimSpace(candidate))
		if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsLoopback() {
			continue
		}
		address := ip.String()
		if seen[address] {
			continue
		}
		seen[address] = true
		servers = append(servers, address)
	}

	return servers
}
