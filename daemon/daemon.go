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
	logManager      *LogManager
}

// NewDaemon creates a new daemon instance
func NewDaemon(configPath string) (*Daemon, error) {
	config, err := core.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	routeManager := core.NewOSRouteManager()
	networkDetector := NewNetworkDetector(config)

	var coordinator *Coordinator
	dnsProxy := core.NewDNSProxy(config, func() *core.Router {
		if coordinator != nil {
			return coordinator.GetActiveRouter()
		}
		return nil
	})

	coordinator = NewCoordinator(config, routeManager, dnsProxy, networkDetector.Observe())
	ipcServer := NewIPCServer(coordinator)
	logManager := NewLogManager()

	return &Daemon{
		config:          config,
		coordinator:     coordinator,
		networkDetector: networkDetector,
		ipcServer:       ipcServer,
		dnsProxy:        dnsProxy,
		logManager:      logManager,
	}, nil
}

// Run starts the daemon and runs until interrupted
func (d *Daemon) Run() error {
	log.Println("Starting Network Router Daemon...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g, gCtx := errgroup.WithContext(ctx)

	// Start log manager
	d.logManager.Start()

	// Start network detector
	g.Go(func() error {
		return d.networkDetector.Start(gCtx)
	})

	// Start coordinator
	g.Go(func() error {
		return d.coordinator.Start(gCtx)
	})

	// Start IPC server
	g.Go(func() error {
		return d.ipcServer.Start(gCtx)
	})

	// Signal handler
	g.Go(func() error {
		return d.handleSignals(gCtx, cancel)
	})

	log.Printf("DNS Proxy configured: Enabled=%v, Port=%d (Managed by Coordinator)", d.config.DNSProxyEnabled, d.config.DNSProxyPort)

	log.Println("✓ Daemon started successfully")

	// Wait for all goroutines
	if err := g.Wait(); err != nil && err != context.Canceled {
		return err
	}

	// Cleanup
	log.Println("Performing cleanup...")
	if err := d.cleanup(); err != nil {
		log.Printf("Cleanup error: %v", err)
	}

	log.Println("Daemon stopped")
	return nil
}

// handleSignals handles OS signals for graceful shutdown
func (d *Daemon) handleSignals(ctx context.Context, cancel context.CancelFunc) error {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		syscall.SIGINT,  // Ctrl+C
		syscall.SIGTERM, // kill command
		syscall.SIGHUP,  // Terminal closed
	)

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
func (d *Daemon) cleanup() error {
	// Stop DNS Proxy
	if d.dnsProxy != nil {
		d.dnsProxy.Stop()
	}
	if d.logManager != nil {
		d.logManager.Stop()
	}
	return nil
}
