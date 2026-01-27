package daemon

import (
	"context"
	"log"
	"network-router/pkg/core"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"
)

// Daemon represents the main daemon process
type Daemon struct {
	config    *core.Config
	state     *RouterState
	monitor   *Monitor
	ipcServer *IPCServer
}

// NewDaemon creates a new daemon instance
func NewDaemon(configPath string) (*Daemon, error) {
	config, err := core.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	state := NewRouterState()
	monitor := NewMonitor(config, state)
	ipcServer := NewIPCServer(monitor, state)

	return &Daemon{
		config:    config,
		state:     state,
		monitor:   monitor,
		ipcServer: ipcServer,
	}, nil
}

// Run starts the daemon and runs until interrupted
func (d *Daemon) Run() error {
	log.Println("Starting Network Router Daemon...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g, gCtx := errgroup.WithContext(ctx)

	// Start network monitor
	g.Go(func() error {
		return d.monitor.Start(gCtx)
	})

	// Start IPC server
	g.Go(func() error {
		return d.ipcServer.Start(gCtx)
	})

	// Signal handler
	g.Go(func() error {
		return d.handleSignals(gCtx, cancel)
	})

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
	// Clear routes if they are applied
	if d.state.AreRoutesApplied() {
		log.Println("Clearing routes before shutdown...")
		if err := d.monitor.ForceClear(); err != nil {
			return err
		}
		log.Println("✓ Routes cleared")
	}
	return nil
}
