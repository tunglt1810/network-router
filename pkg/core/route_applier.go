package core

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"network-router/pkg/utils"

	"gopkg.in/yaml.v3"
)

// RouteKind records why a route exists, so cleanup can report meaningfully and
// so infrastructure routes are never mistaken for user-configured ones.
type RouteKind string

const (
	// RouteStatic covers configured CIDRs and domains resolved at apply time.
	RouteStatic RouteKind = "static"
	// RouteDynamic covers hosts learned from DNS answers at runtime.
	RouteDynamic RouteKind = "dynamic"
	// RouteInfra covers the upstream resolvers the proxy must reach over the
	// tethered path.
	RouteInfra RouteKind = "infrastructure"
)

// ErrPhoneNotReady reports that no usable tethered path is currently available.
var ErrPhoneNotReady = errors.New("phone interface is not ready")

const persistDebounce = 2 * time.Second

// routeState is the on-disk record of routes this process installed.
type routeState struct {
	Routes map[string]string `yaml:"routes"`
}

// RouteApplier owns the routing table entries this product creates, and the
// state file that records them.
//
// It is the single writer. Every mutation takes the same mutex, so route state
// stays consistent even though it is reachable from the DNS handler goroutines
// that the DNS library spawns per query. Those goroutines only enqueue work, so
// answering a query never waits on `route` to finish.
type RouteApplier struct {
	manager   RouteManager
	snapshots *SnapshotStore
	statePath string

	mu      sync.Mutex
	applied map[string]RouteKind

	queue     chan []string
	done      chan struct{}
	closeOnce sync.Once

	persistMu    sync.Mutex
	persistTimer *time.Timer
}

func NewRouteApplier(manager RouteManager, snapshots *SnapshotStore, statePath string) *RouteApplier {
	if manager == nil {
		manager = NewOSRouteManager()
	}
	if snapshots == nil {
		snapshots = NewSnapshotStore()
	}
	if statePath == "" {
		statePath = DefaultStatePath
	}
	return &RouteApplier{
		manager:   manager,
		snapshots: snapshots,
		statePath: statePath,
		applied:   make(map[string]RouteKind),
		queue:     make(chan []string, 256),
		done:      make(chan struct{}),
	}
}

// Start launches the worker that drains dynamically discovered hosts.
func (a *RouteApplier) Start() {
	go a.run()
}

// Stop shuts the worker down and flushes any pending state write.
func (a *RouteApplier) Stop() {
	a.closeOnce.Do(func() { close(a.done) })
	a.flushState()
}

func (a *RouteApplier) run() {
	for {
		select {
		case <-a.done:
			return
		case destinations := <-a.queue:
			a.addBatch(RouteDynamic, destinations)
		}
	}
}

// Submit enqueues hosts learned from a DNS answer. It never blocks the caller:
// the DNS reply must go out without waiting for the routing table.
func (a *RouteApplier) Submit(destinations []string) {
	if len(destinations) == 0 {
		return
	}

	// Filter against the applied set first so a full queue is not consumed by
	// hosts that are already routed.
	pending := make([]string, 0, len(destinations))
	a.mu.Lock()
	for _, destination := range destinations {
		normalized, err := normalizeDestination(destination)
		if err != nil {
			continue
		}
		if _, exists := a.applied[normalized]; !exists {
			pending = append(pending, normalized)
		}
	}
	a.mu.Unlock()

	if len(pending) == 0 {
		return
	}

	select {
	case a.queue <- pending:
	default:
		log.Printf("⚠️ Route queue is full, dropping %d host(s); they will be picked up on the next answer", len(pending))
	}
}

// Add installs a single route synchronously.
func (a *RouteApplier) Add(kind RouteKind, destination string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.addLocked(kind, destination)
}

// AddMany installs routes synchronously and returns every failure encountered.
// Callers decide whether a partial result is acceptable.
func (a *RouteApplier) AddMany(kind RouteKind, destinations []string) []error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var failures []error
	for _, destination := range destinations {
		if err := a.addLocked(kind, destination); err != nil {
			failures = append(failures, err)
		}
	}
	return failures
}

func (a *RouteApplier) addBatch(kind RouteKind, destinations []string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, destination := range destinations {
		if err := a.addLocked(kind, destination); err != nil {
			log.Printf("❌ Dynamic route for %s failed: %v", destination, err)
		}
	}
}

func (a *RouteApplier) addLocked(kind RouteKind, destination string) error {
	normalized, err := normalizeDestination(destination)
	if err != nil {
		return err
	}
	if _, exists := a.applied[normalized]; exists {
		return nil
	}

	gateway, err := a.phoneGateway()
	if err != nil {
		return err
	}

	switch err := a.manager.AddRouteViaGateway(normalized, gateway); {
	case err == nil, errors.Is(err, utils.ErrRouteExists):
		// An entry that already exists is the state we wanted. Recording it
		// anyway brings it into the normal lifecycle, so cleanup removes it
		// instead of leaving it behind.
	default:
		return err
	}

	a.applied[normalized] = kind
	a.schedulePersist()
	return nil
}

// Clear removes every route this applier installed and deletes the state file.
func (a *RouteApplier) Clear() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	destinations := make([]string, 0, len(a.applied))
	for destination := range a.applied {
		destinations = append(destinations, destination)
	}
	sort.Strings(destinations)

	for _, destination := range destinations {
		if err := a.manager.DeleteRoute(destination); err != nil && !errors.Is(err, utils.ErrRouteMissing) {
			log.Printf("⚠️ Could not delete route %s: %v", destination, err)
			continue
		}
		delete(a.applied, destination)
	}

	a.cancelPersist()
	if err := os.Remove(a.statePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ReconcileFromState removes routes recorded by a previous process. Without this
// a crash or an abrupt unplug leaves host routes pointing at a gateway that no
// longer exists, which blackholes exactly the domains the product is meant to
// keep reachable.
func (a *RouteApplier) ReconcileFromState() error {
	state, err := loadRouteState(a.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(state.Routes) == 0 {
		return os.Remove(a.statePath)
	}

	destinations := make([]string, 0, len(state.Routes))
	for destination := range state.Routes {
		destinations = append(destinations, destination)
	}
	sort.Strings(destinations)

	log.Printf("Reconciling %d route(s) left over from a previous run...", len(destinations))
	for _, destination := range destinations {
		if err := a.manager.DeleteRoute(destination); err != nil && !errors.Is(err, utils.ErrRouteMissing) {
			log.Printf("⚠️ Could not remove stale route %s: %v", destination, err)
		}
	}

	if err := os.Remove(a.statePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Count reports how many routes are currently installed.
func (a *RouteApplier) Count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.applied)
}

func (a *RouteApplier) phoneGateway() (string, error) {
	snapshot := a.snapshots.Load()
	if snapshot == nil {
		return "", fmt.Errorf("%w: network has not been discovered yet", ErrPhoneNotReady)
	}
	if !snapshot.PhoneReady {
		reason := snapshot.PhoneReason
		if reason == "" {
			reason = "no usable tethered path"
		}
		return "", fmt.Errorf("%w: %s", ErrPhoneNotReady, reason)
	}
	if err := utils.ValidateGateway(snapshot.PhoneGateway); err != nil {
		return "", fmt.Errorf("%w: %v", ErrPhoneNotReady, err)
	}
	return snapshot.PhoneGateway, nil
}

// schedulePersist debounces state writes so a burst of answers costs one write
// rather than one per record.
func (a *RouteApplier) schedulePersist() {
	a.persistMu.Lock()
	defer a.persistMu.Unlock()

	if a.persistTimer != nil {
		a.persistTimer.Reset(persistDebounce)
		return
	}
	a.persistTimer = time.AfterFunc(persistDebounce, func() {
		if err := a.writeState(); err != nil {
			log.Printf("⚠️ Could not persist route state: %v", err)
		}
	})
}

func (a *RouteApplier) cancelPersist() {
	a.persistMu.Lock()
	defer a.persistMu.Unlock()
	if a.persistTimer != nil {
		a.persistTimer.Stop()
		a.persistTimer = nil
	}
}

func (a *RouteApplier) flushState() {
	a.cancelPersist()
	if a.Count() == 0 {
		return
	}
	if err := a.writeState(); err != nil {
		log.Printf("⚠️ Could not persist route state: %v", err)
	}
}

// writeState snapshots the applied set and writes it atomically, so a crash
// mid-write cannot leave a truncated file that cleanup would misread.
func (a *RouteApplier) writeState() error {
	a.mu.Lock()
	state := routeState{Routes: make(map[string]string, len(a.applied))}
	for destination, kind := range a.applied {
		state.Routes[destination] = string(kind)
	}
	a.mu.Unlock()

	a.persistMu.Lock()
	a.persistTimer = nil
	a.persistMu.Unlock()

	payload, err := yaml.Marshal(state)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(a.statePath), 0o700); err != nil {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(a.statePath), ".state-*.yaml")
	if err != nil {
		return err
	}
	tempName := temp.Name()

	if _, err := temp.Write(payload); err != nil {
		temp.Close()
		os.Remove(tempName)
		return err
	}
	if err := temp.Close(); err != nil {
		os.Remove(tempName)
		return err
	}
	if err := os.Chmod(tempName, 0o600); err != nil {
		os.Remove(tempName)
		return err
	}
	return os.Rename(tempName, a.statePath)
}

func loadRouteState(path string) (*routeState, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state routeState
	if err := yaml.Unmarshal(payload, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// normalizeDestination turns a bare IP into a host route and rejects targets
// that must never be routed over the tethered path. Private ranges are allowed
// on purpose: the configuration legitimately contains internal domains.
func normalizeDestination(destination string) (string, error) {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return "", errors.New("empty route destination")
	}

	if strings.Contains(destination, "/") {
		_, network, err := net.ParseCIDR(destination)
		if err != nil {
			return "", fmt.Errorf("invalid CIDR %q: %w", destination, err)
		}
		if err := validateRoutableIP(network.IP); err != nil {
			return "", err
		}
		return network.String(), nil
	}

	ip := net.ParseIP(destination)
	if ip == nil {
		return "", fmt.Errorf("invalid IP %q", destination)
	}
	if ip.To4() == nil {
		return "", fmt.Errorf("%q is not an IPv4 address", destination)
	}
	if err := validateRoutableIP(ip); err != nil {
		return "", err
	}
	return ip.String() + "/32", nil
}

func validateRoutableIP(ip net.IP) error {
	switch {
	case ip.IsUnspecified():
		return fmt.Errorf("refusing to route unspecified address %s", ip)
	case ip.IsLoopback():
		return fmt.Errorf("refusing to route loopback address %s", ip)
	case ip.IsMulticast():
		return fmt.Errorf("refusing to route multicast address %s", ip)
	case ip.IsLinkLocalUnicast():
		return fmt.Errorf("refusing to route link-local address %s", ip)
	}
	return nil
}
