package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"network-router/pkg/core"
	"os"
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

// IPCServer handles IPC communication
type IPCServer struct {
	monitor  *Monitor
	state    *RouterState
	dnsProxy *core.DNSProxy
	listener net.Listener
}

// NewIPCServer creates a new IPC server
func NewIPCServer(monitor *Monitor, state *RouterState, dnsProxy *core.DNSProxy) *IPCServer {
	return &IPCServer{
		monitor:  monitor,
		state:    state,
		dnsProxy: dnsProxy,
	}
}

// Start starts the IPC server
func (s *IPCServer) Start(ctx context.Context) error {
	// Remove existing socket if present
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to create Unix socket: %w", err)
	}
	s.listener = listener

	// Set permissions to allow user-mode apps (like tray) to connect
	if err := os.Chmod(socketPath, 0666); err != nil {
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	log.Printf("IPC server listening on %s", socketPath)

	// Accept connections in a goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("Accept error: %v", err)
					continue
				}
			}
			go s.handleConnection(conn)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	log.Println("IPC server stopping...")
	return s.Stop()
}

// Stop stops the IPC server
func (s *IPCServer) Stop() error {
	if s.listener != nil {
		s.listener.Close()
	}
	os.Remove(socketPath)
	return nil
}

// handleConnection handles a single client connection
func (s *IPCServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	var req IPCRequest
	if err := decoder.Decode(&req); err != nil {
		log.Printf("Decode error: %v", err)
		s.sendResponse(conn, IPCResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	if req.Action != "status" {
		log.Printf("IPC request: %s", req.Action)
	}

	response := s.processRequest(req)
	s.sendResponse(conn, response)
}

// processRequest processes an IPC request and returns a response
func (s *IPCServer) processRequest(req IPCRequest) IPCResponse {
	switch req.Action {
	case "status":
		return IPCResponse{
			Success: true,
			Data:    s.state.GetStatus(),
		}

	case "enable":
		s.state.SetAutoRouting(true)
		// DNS Proxy lifecycle is now managed by Monitor based on routing status
		return IPCResponse{
			Success: true,
			Message: "Auto-routing enabled",
		}

	case "disable":
		s.state.SetAutoRouting(false)
		// DNS Proxy lifecycle is now managed by Monitor based on routing status
		return IPCResponse{
			Success: true,
			Message: "Auto-routing disabled",
		}

	case "apply":
		if err := s.monitor.ForceApply(); err != nil {
			return IPCResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to apply routes: %v", err),
			}
		}
		return IPCResponse{
			Success: true,
			Message: "Routes applied successfully",
		}

	case "clear":
		autoRoutingWasEnabled := s.state.IsAutoRoutingEnabled()
		if err := s.monitor.ForceClear(); err != nil {
			return IPCResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to clear routes: %v", err),
			}
		}
		msg := "Routes cleared successfully"
		if autoRoutingWasEnabled {
			msg += " (auto-routing disabled)"
		}
		return IPCResponse{
			Success: true,
			Message: msg,
		}

	case "restart":
		if err := s.monitor.ForceClear(); err != nil {
			return IPCResponse{
				Success: false,
				Message: fmt.Sprintf("Restart failed at clear: %v", err),
			}
		}
		if err := s.monitor.ForceApply(); err != nil {
			return IPCResponse{
				Success: false,
				Message: fmt.Sprintf("Restart failed at apply: %v", err),
			}
		}
		return IPCResponse{
			Success: true,
			Message: "Routes restarted successfully",
		}

	case "refresh":
		s.monitor.RefreshRoutes()
		return IPCResponse{
			Success: true,
			Message: "Refresh triggered",
		}

	case "enable_dns_proxy":
		if s.dnsProxy != nil {
			if err := s.dnsProxy.Start(); err != nil {
				return IPCResponse{
					Success: false,
					Message: fmt.Sprintf("Failed to start DNS Proxy: %v", err),
				}
			}
			s.state.SetDNSProxyEnabled(true)
			return IPCResponse{
				Success: true,
				Message: "DNS Proxy enabled",
			}
		}
		return IPCResponse{
			Success: false,
			Message: "DNS Proxy not initialized",
		}

	case "disable_dns_proxy":
		if s.dnsProxy != nil {
			if err := s.dnsProxy.Stop(); err != nil {
				return IPCResponse{
					Success: false,
					Message: fmt.Sprintf("Failed to stop DNS Proxy: %v", err),
				}
			}
			s.state.SetDNSProxyEnabled(false)
			return IPCResponse{
				Success: true,
				Message: "DNS Proxy disabled",
			}
		}
		return IPCResponse{
			Success: false,
			Message: "DNS Proxy not initialized",
		}

	default:
		return IPCResponse{
			Success: false,
			Message: fmt.Sprintf("Unknown action: %s", req.Action),
		}
	}
}

// sendResponse sends a response to the client
func (s *IPCServer) sendResponse(conn net.Conn, response IPCResponse) {
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(response); err != nil {
		log.Printf("Failed to send response: %v", err)
	}
}
