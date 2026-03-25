package client

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"network-router/daemon"
)

const socketPath = "/tmp/network-router.sock"

// IPCRequest represents a client request
type IPCRequest struct {
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"params,omitempty"`
}

// IPCResponse represents a server response
type IPCResponse struct {
	Success bool                 `json:"success"`
	Message string               `json:"message,omitempty"`
	Data    *daemon.RouterStatus `json:"data,omitempty"`
}

// Client handles communication with the daemon
type Client struct {
	socketPath string
	timeout    time.Duration
}

// NewClient creates a new client
func NewClient() *Client {
	return &Client{
		socketPath: socketPath,
		timeout:    5 * time.Second,
	}
}

// SendRequest sends a request to the daemon and returns the response
func (c *Client) SendRequest(action string, params map[string]interface{}) (*IPCResponse, error) {
	// Use longer timeout for restart command as it involves clearing + applying
	timeout := c.timeout
	if action == daemon.ActionRestart || action == daemon.ActionApply || action == daemon.ActionRefresh {
		timeout = 120 * time.Second
	}

	conn, err := net.DialTimeout("unix", c.socketPath, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon (is it running?): %w", err)
	}
	defer conn.Close()

	// Set deadline for entire operation
	conn.SetDeadline(time.Now().Add(timeout))

	req := IPCRequest{
		Action: action,
		Params: params,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var response IPCResponse
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to receive response: %w", err)
	}

	return &response, nil
}

// Status gets the current daemon status
func (c *Client) Status() error {
	resp, err := c.SendRequest(daemon.ActionStatus, nil)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("status request failed: %s", resp.Message)
	}

	fmt.Println("Daemon Status:")
	fmt.Println("================")

	if data := resp.Data; data != nil {
		fmt.Printf("Auto-routing:     %v\n", data.AutoRoutingEnabled)
		fmt.Printf("Routes applied:   %v\n", data.RoutesApplied)
		fmt.Printf("WiFi active:      %v\n", data.WifiActive)
		fmt.Printf("Phone active:     %v\n", data.PhoneActive)

		if !data.LastAppliedAt.IsZero() {
			fmt.Printf("Last applied:     %s\n", data.LastAppliedAt.Format(time.RFC3339))
		}
		if !data.LastClearedAt.IsZero() {
			fmt.Printf("Last cleared:     %s\n", data.LastClearedAt.Format(time.RFC3339))
		}
		fmt.Printf("DNS Proxy:        %v\n", data.DNSProxyEnabled)
		fmt.Printf("Auto Refresh:     %v\n", data.AutoRefreshRouteEnabled)
	}

	return nil
}

// Enable enables auto-routing
func (c *Client) Enable() error {
	resp, err := c.SendRequest(daemon.ActionEnable, nil)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("enable request failed: %s", resp.Message)
	}

	fmt.Println("✓ Auto-routing enabled")
	return nil
}

// Disable disables auto-routing
func (c *Client) Disable() error {
	resp, err := c.SendRequest(daemon.ActionDisable, nil)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("disable request failed: %s", resp.Message)
	}

	fmt.Println("✓ Auto-routing disabled")
	return nil
}

// Apply forces route application
func (c *Client) Apply() error {
	fmt.Println("Applying routes...")
	resp, err := c.SendRequest(daemon.ActionApply, nil)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("apply request failed: %s", resp.Message)
	}

	fmt.Println("✓", resp.Message)
	return nil
}

// Clear forces route clearing
func (c *Client) Clear() error {
	fmt.Println("Clearing routes...")
	resp, err := c.SendRequest(daemon.ActionClear, nil)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("clear request failed: %s", resp.Message)
	}

	fmt.Println("✓", resp.Message)
	return nil
}

// Restart clears and re-applies routes
func (c *Client) Restart() error {
	fmt.Println("Restarting routes...")
	resp, err := c.SendRequest(daemon.ActionRestart, nil)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("restart request failed: %s", resp.Message)
	}

	fmt.Println("✓", resp.Message)
	return nil
}

// EnableDNSProxy enables the DNS proxy
func (c *Client) EnableDNSProxy() error {
	resp, err := c.SendRequest(daemon.ActionEnableDNSProxy, nil)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("enable_dns_proxy request failed: %s", resp.Message)
	}

	fmt.Println("✓ DNS Proxy enabled")
	return nil
}

// DisableDNSProxy disables the DNS proxy
func (c *Client) DisableDNSProxy() error {
	resp, err := c.SendRequest(daemon.ActionDisableDNSProxy, nil)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("disable_dns_proxy request failed: %s", resp.Message)
	}

	fmt.Println("✓ DNS Proxy disabled")
	return nil
}
