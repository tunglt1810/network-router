package client

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

const socketPath = "/tmp/network-router.sock"

// IPCRequest represents a client request
type IPCRequest struct {
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"params,omitempty"`
}

// IPCResponse represents a server response
type IPCResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"`
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
	if action == "restart" || action == "apply" || action == "refresh" {
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
	resp, err := c.SendRequest("status", nil)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("status request failed: %s", resp.Message)
	}

	fmt.Println("Daemon Status:")
	fmt.Println("================")

	if data := resp.Data; data != nil {
		fmt.Printf("Auto-routing:     %v\n", data["auto_routing_enabled"])
		fmt.Printf("Routes applied:   %v\n", data["routes_applied"])
		fmt.Printf("WiFi active:      %v\n", data["wifi_active"])
		fmt.Printf("Phone active:     %v\n", data["phone_active"])

		if lastApplied, ok := data["last_applied_at"].(string); ok && lastApplied != "" {
			fmt.Printf("Last applied:     %s\n", lastApplied)
		}
		if lastCleared, ok := data["last_cleared_at"].(string); ok && lastCleared != "" {
			fmt.Printf("Last cleared:     %s\n", lastCleared)
		}
	}

	return nil
}

// Enable enables auto-routing
func (c *Client) Enable() error {
	resp, err := c.SendRequest("enable", nil)
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
	resp, err := c.SendRequest("disable", nil)
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
	resp, err := c.SendRequest("apply", nil)
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
	resp, err := c.SendRequest("clear", nil)
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
	resp, err := c.SendRequest("restart", nil)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("restart request failed: %s", resp.Message)
	}

	fmt.Println("✓", resp.Message)
	return nil
}
