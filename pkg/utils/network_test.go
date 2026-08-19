package utils

import "testing"

func TestParseHardwarePortsPreservesDeviceRemapping(t *testing.T) {
	output := `Hardware Port: Wi-Fi
Device: en0
Ethernet Address: aa:bb:cc:dd:ee:00

Hardware Port: iPhone USB
Device: en8
Ethernet Address: aa:bb:cc:dd:ee:08
`

	interfaces := ParseHardwarePorts(output)
	matches := FindInterfacesByName(interfaces, []string{"iPhone USB"})
	if len(matches) != 1 {
		t.Fatalf("expected exactly one iPhone USB interface, got %d", len(matches))
	}
	if matches[0].DeviceName != "en8" {
		t.Fatalf("expected remapped device en8, got %s", matches[0].DeviceName)
	}
}

func TestHasUsableIPv4RejectsLinkLocalOnlyInterface(t *testing.T) {
	if HasUsableIPv4([]string{"169.254.134.156"}) {
		t.Fatal("link-local address must not make an interface usable")
	}
	if !HasUsableIPv4([]string{"172.20.10.2"}) {
		t.Fatal("expected routable IPv4 address to be usable")
	}
}

func TestParseIPv4AddressesIgnoresIPv6(t *testing.T) {
	output := `en8: flags=8863<UP,BROADCAST,RUNNING>
	inet6 fe80::1%en8 prefixlen 64
	inet 172.20.10.2 netmask 0xffffff00 broadcast 172.20.10.255
`

	addresses := ParseIPv4Addresses(output)
	if len(addresses) != 1 || addresses[0] != "172.20.10.2" {
		t.Fatalf("unexpected addresses: %#v", addresses)
	}
}
