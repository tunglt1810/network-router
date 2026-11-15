package daemon

import (
	"errors"
	"testing"

	"network-router/pkg/core"
	"network-router/pkg/utils"
)

type detectorNetworkProvider struct {
	err error
}

func (p *detectorNetworkProvider) GetNetworkInterfaces() ([]utils.InterfaceInfo, error) {
	return nil, p.err
}

func (p *detectorNetworkProvider) GetInterfaceIPv4Addresses(deviceName string) ([]string, error) {
	return nil, nil
}

func (p *detectorNetworkProvider) GetInterfaceGateway(deviceName string) (string, error) {
	return "", nil
}

func TestNetworkDetectorEmitsNotReadySnapshotWhenDiscoveryFails(t *testing.T) {
	snapshots := core.NewSnapshotStore()
	detector := NewNetworkDetector(&core.Config{}, &detectorNetworkProvider{
		err: errors.New("networksetup unavailable"),
	}, snapshots)

	detector.check()

	snapshot := <-detector.Observe()
	if snapshot.PhoneReady {
		t.Fatalf("expected fail-closed snapshot: %#v", snapshot)
	}
	if snapshot.PhoneReason == "" {
		t.Fatal("expected discovery failure reason")
	}
}

// The store must carry the same snapshot, because the DNS request path reads the
// routing target from there instead of rediscovering interfaces per answer.
func TestNetworkDetectorPublishesToSnapshotStore(t *testing.T) {
	snapshots := core.NewSnapshotStore()
	detector := NewNetworkDetector(&core.Config{}, &detectorNetworkProvider{
		err: errors.New("networksetup unavailable"),
	}, snapshots)

	detector.check()

	if snapshots.Load() == nil {
		t.Fatal("expected the detector to publish a snapshot")
	}
	if snapshots.Load().PhoneReady {
		t.Fatal("expected the published snapshot to be fail-closed")
	}
}
