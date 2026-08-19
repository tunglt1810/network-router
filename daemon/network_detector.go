package daemon

import (
	"context"
	"fmt"
	"log"
	"time"

	"network-router/pkg/core"
)

// NetworkDetector periodically polls the OS for network interface changes,
// publishes the result to the shared SnapshotStore, and notifies the coordinator.
//
// Publishing to the store is what lets the DNS request path read the current
// routing target without shelling out to networksetup/ifconfig per answer.
type NetworkDetector struct {
	config        *core.Config
	provider      core.NetworkProvider
	snapshots     *core.SnapshotStore
	checkInterval time.Duration
	events        chan *core.NetworkSnapshot
}

func NewNetworkDetector(config *core.Config, provider core.NetworkProvider, snapshots *core.SnapshotStore) *NetworkDetector {
	if provider == nil {
		provider = core.NewOSNetworkProvider()
	}
	if snapshots == nil {
		snapshots = core.NewSnapshotStore()
	}
	return &NetworkDetector{
		config:        config,
		provider:      provider,
		snapshots:     snapshots,
		checkInterval: 5 * time.Second,
		events:        make(chan *core.NetworkSnapshot, 1),
	}
}

// Observe returns a read-only channel of network snapshots.
func (d *NetworkDetector) Observe() <-chan *core.NetworkSnapshot {
	return d.events
}

func (d *NetworkDetector) Start(ctx context.Context) error {
	log.Println("Starting network detector...")
	ticker := time.NewTicker(d.checkInterval)
	defer ticker.Stop()

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
	snapshot, err := core.DiscoverNetwork(d.config, d.provider)
	if err != nil {
		log.Printf("Network check error: %v", err)
		snapshot = &core.NetworkSnapshot{
			PhoneReason: fmt.Sprintf("network discovery failed: %v", err),
		}
	}

	d.snapshots.Store(snapshot)
	d.emit(snapshot)
}

func (d *NetworkDetector) emit(snapshot *core.NetworkSnapshot) {
	// Keep only the newest snapshot: an older one is never useful.
	select {
	case d.events <- snapshot:
	default:
		select {
		case <-d.events:
		default:
		}
		select {
		case d.events <- snapshot:
		default:
		}
	}
}
