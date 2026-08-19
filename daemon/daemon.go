package daemon

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"network-router/pkg/core"

	"golang.org/x/sync/errgroup"
)

// Daemon represents the main daemon process
type Daemon struct {
	config          *core.Config
	coordinator     *Coordinator
	networkDetector *NetworkDetector
	ipcServer       *IPCServer
	dnsProxy        *core.DNSProxy
	applier         *core.RouteApplier
	logManager      *LogManager
}

// NewDaemon creates a new daemon instance
func NewDaemon(configPath string) (*Daemon, error) {
	config, err := core.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	provider := core.NewOSNetworkProvider()
	snapshots := core.NewSnapshotStore()
	applier := core.NewRouteApplier(core.NewOSRouteManager(), snapshots, config.ResolvedStatePath())
	router := core.NewRouter(config, provider, snapshots, applier)
	upstream := core.NewUpstreamResolver(config, snapshots)
	dnsProxy := core.NewDNSProxy(config, upstream, applier)
	detector := NewNetworkDetector(config, provider, snapshots)
	coordinator := NewCoordinator(config, router, applier, dnsProxy, snapshots, detector.Observe())

	return &Daemon{
		config:          config,
		coordinator:     coordinator,
		networkDetector: detector,
		ipcServer:       NewIPCServer(coordinator),
		dnsProxy:        dnsProxy,
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

	log.Printf("DNS Proxy configured: Enabled=%v, Port=%d, ForceIPv4=%v",
		d.config.DNSProxyEnabled, d.config.ProxyPort(), d.config.ForceIPv4Enabled())
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
	if d.dnsProxy != nil {
		if err := d.dnsProxy.Stop(); err != nil {
			log.Printf("DNS proxy shutdown error: %v", err)
		}
	}
	if d.applier != nil {
		d.applier.Stop()
	}
	if d.logManager != nil {
		d.logManager.Stop()
	}
}
