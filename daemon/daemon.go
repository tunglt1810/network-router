package daemon

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"network-router/pkg/core"
	"network-router/pkg/utils"

	"golang.org/x/sync/errgroup"
)

// Daemon represents the main daemon process
type Daemon struct {
	coordinator     *Coordinator
	networkDetector *NetworkDetector
	ipcServer       *IPCServer
	applier         *core.RouteApplier
	logManager      *LogManager
}

// NewDaemon creates a new daemon instance
func NewDaemon(configPath string) (*Daemon, error) {
	config, err := core.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	system := utils.System{}
	snapshots := core.NewSnapshotStore()
	applier := core.NewRouteApplier(system, snapshots, core.StatePath)
	router := core.NewRouter(config, applier)
	upstream := core.NewUpstreamResolver(config, snapshots)
	dnsProxy := core.NewDNSProxy(config, upstream, applier)
	detector := NewNetworkDetector(config, system, snapshots)
	coordinator := NewCoordinator(router, applier, dnsProxy, snapshots, detector.Observe())

	return &Daemon{
		coordinator:     coordinator,
		networkDetector: detector,
		ipcServer:       NewIPCServer(coordinator),
		applier:         applier,
		logManager:      NewLogManager(),
	}, nil
}

// Run starts the daemon and runs until interrupted
func (d *Daemon) Run() error {
	log.Println("Starting Network Router Daemon...")

	// Reconcile before anything else. A previous process may have been killed
	// while resolver files were installed and host routes were in the table;
	// both would otherwise blackhole the configured domains.
	if err := core.ReconcileResolvers(); err != nil {
		log.Printf("Warning: could not reconcile resolver files: %v", err)
	}
	if err := d.applier.ReconcileFromState(); err != nil {
		log.Printf("Warning: could not reconcile leftover routes: %v", err)
	}
	d.applier.Start()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g, gCtx := errgroup.WithContext(ctx)

	d.logManager.Start()

	g.Go(func() error { return d.networkDetector.Start(gCtx) })
	g.Go(func() error { return d.coordinator.Start(gCtx) })
	g.Go(func() error { return d.ipcServer.Start(gCtx) })
	g.Go(func() error { return d.handleSignals(gCtx, cancel) })

	log.Println("✓ Daemon started successfully")

	if err := g.Wait(); err != nil && err != context.Canceled {
		return err
	}

	log.Println("Performing cleanup...")
	d.cleanup()
	log.Println("Daemon stopped")
	return nil
}

// handleSignals handles OS signals for graceful shutdown
func (d *Daemon) handleSignals(ctx context.Context, cancel context.CancelFunc) error {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case sig := <-sigChan:
		log.Printf("Received signal: %v, initiating graceful shutdown...", sig)
		cancel()
		return nil
	}
}

// cleanup performs cleanup operations before shutdown
func (d *Daemon) cleanup() {
	// Routes point at the phone gateway. Left in place after the daemon stops,
	// they blackhole their destinations once tethering goes away, and only the
	// next start would reconcile them.
	if err := d.coordinator.Shutdown(); err != nil {
		log.Printf("Route cleanup error: %v", err)
	}
	if d.applier != nil {
		d.applier.Stop()
	}
	if d.logManager != nil {
		d.logManager.Stop()
	}
}
