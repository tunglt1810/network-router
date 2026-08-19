package core

import (
	"testing"

	"network-router/pkg/utils"
)

type fakeNetworkProvider struct {
	interfaces []utils.InterfaceInfo
	addresses  map[string][]string
	gateways   map[string]string
}

func (f *fakeNetworkProvider) GetNetworkInterfaces() ([]utils.InterfaceInfo, error) {
	return f.interfaces, nil
}

func (f *fakeNetworkProvider) GetInterfaceIPv4Addresses(deviceName string) ([]string, error) {
	return f.addresses[deviceName], nil
}

func (f *fakeNetworkProvider) GetInterfaceGateway(deviceName string) (string, error) {
	return f.gateways[deviceName], nil
}

func TestDiscoverNetworkSelectsRemappedPhoneDevice(t *testing.T) {
	provider := &fakeNetworkProvider{
		interfaces: []utils.InterfaceInfo{
			{Name: "Wi-Fi", DeviceName: "en0"},
			{Name: "iPhone USB", DeviceName: "en8"},
		},
		addresses: map[string][]string{
			"en0": {"10.0.0.2"},
			"en8": {"172.20.10.2"},
		},
		gateways: map[string]string{
			"en0": "10.0.0.1",
			"en8": "172.20.10.1",
		},
	}

	snapshot, err := DiscoverNetwork(&Config{}, provider)
	if err != nil {
		t.Fatalf("discover network: %v", err)
	}
	if !snapshot.PhoneReady || snapshot.PhoneInterface.DeviceName != "en8" {
		t.Fatalf("unexpected phone snapshot: %#v", snapshot)
	}
}

func TestDiscoverNetworkRejectsPhoneWithoutUsableGateway(t *testing.T) {
	provider := &fakeNetworkProvider{
		interfaces: []utils.InterfaceInfo{
			{Name: "Wi-Fi", DeviceName: "en0"},
			{Name: "iPhone USB", DeviceName: "en7"},
		},
		addresses: map[string][]string{
			"en0": {"10.0.0.2"},
			"en7": {"169.254.134.156"},
		},
		gateways: map[string]string{
			"en0": "10.0.0.1",
		},
	}

	snapshot, err := DiscoverNetwork(&Config{}, provider)
	if err != nil {
		t.Fatalf("discover network: %v", err)
	}
	if snapshot.PhoneReady {
		t.Fatalf("expected phone path to be unavailable: %#v", snapshot)
	}
	if snapshot.PhoneReason == "" {
		t.Fatal("expected phone readiness reason")
	}
}

func TestDiscoverNetworkDoesNotSelectUnknownActiveInterface(t *testing.T) {
	provider := &fakeNetworkProvider{
		interfaces: []utils.InterfaceInfo{
			{Name: "Wi-Fi", DeviceName: "en0"},
			{Name: "USB 10/100 LAN", DeviceName: "en8"},
		},
		addresses: map[string][]string{
			"en0": {"10.0.0.2"},
			"en8": {"172.20.10.2"},
		},
		gateways: map[string]string{
			"en0": "10.0.0.1",
			"en8": "172.20.10.1",
		},
	}

	snapshot, err := DiscoverNetwork(&Config{}, provider)
	if err != nil {
		t.Fatalf("discover network: %v", err)
	}
	if snapshot.PhoneReady {
		t.Fatal("unknown active interface must not be selected as phone path")
	}
}
