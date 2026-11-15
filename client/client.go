package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"network-router/daemon"
)

// Client handles communication with the daemon
type Client struct {
	socketPath string
	timeout    time.Duration
}

// NewClient creates a new client
func NewClient() *Client {
	return &Client{
		socketPath: daemon.SocketPath,
		timeout:    5 * time.Second,
	}
}

// requestTimeout returns the timeout for an action. Route operations resolve
// domains and wait behind each other in the daemon, so they get a long timeout.
func (c *Client) requestTimeout(action string) time.Duration {
	if action == daemon.ActionStatus {
		return c.timeout
	}
	return 120 * time.Second
}

// SendRequest sends a request to the daemon and returns the response
func (c *Client) SendRequest(action string) (*daemon.IPCResponse, error) {
	timeout := c.requestTimeout(action)

	conn, err := net.DialTimeout("unix", c.socketPath, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon (is it running?): %w", err)
	}
	defer conn.Close()

	// Set deadline for entire operation
	conn.SetDeadline(time.Now().Add(timeout))

	req := daemon.IPCRequest{Action: action}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var response daemon.IPCResponse
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to receive response: %w", err)
	}

	return &response, nil
}

// Commands maps each CLI command, other than status, to its IPC action. The
// older command names stay as aliases, so existing scripts keep working.
var Commands = map[string]string{
	"on":      daemon.ActionOn,
	"off":     daemon.ActionOff,
	"refresh": daemon.ActionRefresh,
	"enable":  daemon.ActionOn,
	"apply":   daemon.ActionOn,
	"restart": daemon.ActionOn,
	"disable": daemon.ActionOff,
	"clear":   daemon.ActionOff,
}

// Run sends an action to the daemon and prints the daemon's message.
func (c *Client) Run(action string) error {
	resp, err := c.SendRequest(action)
	if err != nil {
		return err
	}
	if !resp.Success {
		return errors.New(resp.Message)
	}
	fmt.Println("✓", resp.Message)
	return nil
}

// Status gets the current daemon status
func (c *Client) Status() error {
	resp, err := c.SendRequest(daemon.ActionStatus)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("status request failed: %s", resp.Message)
	}

	fmt.Println("Daemon Status:")
	fmt.Println("================")

	if data := resp.Data; data != nil {
		fmt.Printf("Routing on:       %v\n", data.AutoRoutingEnabled)
		fmt.Printf("Routes applied:   %v\n", data.RoutesApplied)
		fmt.Printf("Phone interface:  %s\n", data.PhoneDevice)
		fmt.Printf("Phone gateway:    %s\n", data.PhoneGateway)
		fmt.Printf("Phone ready:      %v\n", data.PhoneReady)
		if data.PhoneStatusReason != "" {
			fmt.Printf("Phone reason:     %s\n", data.PhoneStatusReason)
		}
		fmt.Printf("Active routes:    %d\n", data.ActiveRoutes)

		if !data.LastAppliedAt.IsZero() {
			fmt.Printf("Last applied:     %s\n", data.LastAppliedAt.Format(time.RFC3339))
		}
		if !data.LastClearedAt.IsZero() {
			fmt.Printf("Last cleared:     %s\n", data.LastClearedAt.Format(time.RFC3339))
		}
		fmt.Printf("DNS proxy:        %v\n", data.DNSProxyRunning)
	}

	return nil
}
