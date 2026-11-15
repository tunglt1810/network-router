package core

import (
	"fmt"
	"strings"
	"sync/atomic"

	"network-router/pkg/utils"
)

// NetworkProvider abstracts macOS network inspection so discovery can be tested
// without depending on the host's current interface numbering.
type NetworkProvider interface {
	GetNetworkInterfaces() ([]utils.InterfaceInfo, error)
	GetInterfaceIPv4Addresses(deviceName string) ([]string, error)
	GetInterfaceGateway(deviceName string) (string, error)
}

// NetworkSnapshot contains the current routing-path discovery result.
//
// It is immutable once published. Readers on the DNS request path must take the
// routing target from here rather than discovering it themselves: discovery
// shells out to networksetup, ifconfig, and ipconfig, which is far too slow to
// sit inside the resolver's timeout budget.
type NetworkSnapshot struct {
	PhoneInterface *utils.InterfaceInfo
	PhoneGateway   string
	PhoneReady     bool
	PhoneReason    string
}

// PhoneDevice returns the phone device name, or an empty string.
func (s *NetworkSnapshot) PhoneDevice() string {
	if s == nil || s.PhoneInterface == nil {
		return ""
	}
	return s.PhoneInterface.DeviceName
}

// SnapshotStore publishes the latest NetworkSnapshot to every reader without
// locking. The detector is the only writer.
type SnapshotStore struct {
	value atomic.Pointer[NetworkSnapshot]
}

func NewSnapshotStore() *SnapshotStore {
	return &SnapshotStore{}
}

func (s *SnapshotStore) Store(snapshot *NetworkSnapshot) {
	s.value.Store(snapshot)
}

func (s *SnapshotStore) Load() *NetworkSnapshot {
	return s.value.Load()
}

// DiscoverNetwork resolves the current phone service candidate. It never
// chooses an arbitrary interface: only configured phone keywords and known
// tethering service names are considered.
func DiscoverNetwork(config *Config, provider NetworkProvider) (*NetworkSnapshot, error) {

	interfaces, err := provider.GetNetworkInterfaces()
	if err != nil {
		return nil, fmt.Errorf("get network interfaces: %w", err)
	}

	phoneKeyword := config.PhoneInterfaceKeyword
	if phoneKeyword == "" {
		phoneKeyword = "iPhone USB"
	}

	snapshot := &NetworkSnapshot{}
	phoneCandidates := utils.FindInterfacesByName(interfaces, []string{
		phoneKeyword,
		"iPhone USB",
		"iPad USB",
		"RNDIS",
	})

	snapshot.PhoneInterface, snapshot.PhoneReady, snapshot.PhoneGateway, snapshot.PhoneReason = selectPhoneInterface(phoneCandidates, provider)

	return snapshot, nil
}

func selectPhoneInterface(candidates []utils.InterfaceInfo, provider NetworkProvider) (*utils.InterfaceInfo, bool, string, string) {
	if len(candidates) == 0 {
		return nil, false, "", "no iPhone USB, iPad USB, RNDIS, or configured phone interface was found"
	}

	var first *utils.InterfaceInfo
	var lastReason string
	for i := range candidates {
		candidate := candidates[i]
		if first == nil {
			first = &candidate
		}

		addresses, err := provider.GetInterfaceIPv4Addresses(candidate.DeviceName)
		if err != nil {
			lastReason = fmt.Sprintf("phone interface %s cannot be inspected: %v", candidate.DeviceName, err)
			continue
		}
		if !utils.HasUsableIPv4(addresses) {
			lastReason = fmt.Sprintf("phone interface %s has no usable IPv4 address", candidate.DeviceName)
			continue
		}

		gateway, err := provider.GetInterfaceGateway(candidate.DeviceName)
		gateway = strings.TrimSpace(gateway)
		if err != nil || gateway == "" {
			if err != nil {
				lastReason = fmt.Sprintf("phone interface %s has no gateway: %v", candidate.DeviceName, err)
			} else {
				lastReason = fmt.Sprintf("phone interface %s has no gateway", candidate.DeviceName)
			}
			continue
		}
		if err := utils.ValidateGateway(gateway); err != nil {
			lastReason = fmt.Sprintf("phone interface %s: %v", candidate.DeviceName, err)
			continue
		}

		return &candidate, true, gateway, ""
	}

	if lastReason == "" {
		lastReason = "no usable phone interface was found"
	}
	return first, false, "", lastReason
}
