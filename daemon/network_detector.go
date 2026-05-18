package daemon

import (
	"context"
	"log"
	"time"

	"network-router/pkg/core"
	"network-router/pkg/utils"
)

// NetworkEvent is emitted when the network status is checked
type NetworkEvent struct {
	WifiActive  bool
	PhoneActive bool
}

// NetworkDetector periodically polls the OS for network interface changes
// and emits NetworkEvents.
type NetworkDetector struct {
	config        *core.Config
	checkInterval time.Duration
	events        chan NetworkEvent
}

func NewNetworkDetector(config *core.Config) *NetworkDetector {
	return &NetworkDetector{
		config:        config,
		checkInterval: 5 * time.Second,
		events:        make(chan NetworkEvent, 1),
	}
}

// Observe returns a read-only channel for NetworkEvents
func (d *NetworkDetector) Observe() <-chan NetworkEvent {
	return d.events
}

func (d *NetworkDetector) Start(ctx context.Context) error {
	log.Println("Starting network detector...")
	ticker := time.NewTicker(d.checkInterval)
	defer ticker.Stop()

	// Initial check
	d.check()

	for {
		select {
		case <-ctx.Done():
			log.Println("Network detector stopping...")
			return ctx.Err()
		case <-ticker.C:
			d.check()
		}
	}
}

func (d *NetworkDetector) check() {
	interfaces, err := utils.GetNetworkInterfaces()
	if err != nil {
		log.Printf("Network check error: %v", err)
		return
	}

	wifiKw := d.config.WifiInterfaceKeyword
	if wifiKw == "" {
		wifiKw = "Wi-Fi"
	}
	phoneKw := d.config.PhoneInterfaceKeyword
	if phoneKw == "" {
		phoneKw = "iPhone USB"
	}

	wifiIface := utils.FindInterfaceByName(interfaces, []string{wifiKw, "Wi-Fi"})
	phoneIface := utils.FindInterfaceByName(interfaces, []string{phoneKw, "iPhone USB", "iPad USB", "RNDIS"})

	wifiActive := false
	phoneActive := false

	if wifiIface != nil {
		wifiActive = utils.IsInterfaceActive(wifiIface.DeviceName)
	}
	if phoneIface != nil {
		phoneActive = utils.IsInterfaceActive(phoneIface.DeviceName)
	}

	// Send event non-blocking (replace old event if channel is full)
	select {
	case d.events <- NetworkEvent{WifiActive: wifiActive, PhoneActive: phoneActive}:
	default:
		// Drain and replace
		select {
		case <-d.events:
		default:
		}
		d.events <- NetworkEvent{WifiActive: wifiActive, PhoneActive: phoneActive}
	}
}
