package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
)

const socketPath = "/tmp/network-router.sock"

// IPC Action Constants
const (
	ActionStatus             = "status"
	ActionEnable             = "enable"
	ActionDisable            = "disable"
	ActionApply              = "apply"
	ActionClear              = "clear"
	ActionRestart            = "restart"
	ActionRefresh            = "refresh"
	ActionEnableDNSProxy     = "enable_dns_proxy"
	ActionDisableDNSProxy    = "disable_dns_proxy"
	ActionEnableAutoRefresh  = "enable_auto_refresh"
	ActionDisableAutoRefresh = "disable_auto_refresh"
)

// IPCRequest represents a client request
type IPCRequest struct {
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"params,omitempty"`
}

// IPCResponse represents a server response
type IPCResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message,omitempty"`
	Data    *RouterStatus `json:"data,omitempty"`
}

// IPCServer handles IPC communication
type IPCServer struct {
	coordinator *Coordinator
	listener    net.Listener
}

// NewIPCServer creates a new IPC server
func NewIPCServer(coordinator *Coordinator) *IPCServer {
	return &IPCServer{
		coordinator: coordinator,
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

	if req.Action != ActionStatus {
		log.Printf("IPC request: %s", req.Action)
	}

	response := s.processRequest(req)
	s.sendResponse(conn, response)
}

// processRequest processes an IPC request and returns a response
func (s *IPCServer) processRequest(req IPCRequest) IPCResponse {
	switch req.Action {
	case ActionStatus:
		return IPCResponse{
			Success: true,
			Data:    s.coordinator.GetStatus(),
		}

	case ActionEnable:
		s.coordinator.SetAutoRouting(true)
		return IPCResponse{
			Success: true,
			Message: "Auto-routing enabled",
		}

	case ActionDisable:
		s.coordinator.SetAutoRouting(false)
		return IPCResponse{
			Success: true,
			Message: "Auto-routing disabled",
		}

	case ActionApply:
		if err := s.coordinator.ForceApply(); err != nil {
			return IPCResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to apply routes: %v", err),
			}
		}
		return IPCResponse{
			Success: true,
			Message: "Routes applied successfully",
		}

	case ActionClear:
		autoRoutingWasEnabled := s.coordinator.GetStatus().AutoRoutingEnabled
		if err := s.coordinator.ForceClear(); err != nil {
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

	case ActionRestart:
		if err := s.coordinator.ForceClear(); err != nil {
			return IPCResponse{
				Success: false,
				Message: fmt.Sprintf("Restart failed at clear: %v", err),
			}
		}
		if err := s.coordinator.ForceApply(); err != nil {
			return IPCResponse{
				Success: false,
				Message: fmt.Sprintf("Restart failed at apply: %v", err),
			}
		}
		return IPCResponse{
			Success: true,
			Message: "Routes restarted successfully",
		}

	case ActionRefresh:
		s.coordinator.RefreshRoutes()
		return IPCResponse{
			Success: true,
			Message: "Refresh triggered",
		}

	case ActionEnableDNSProxy:
		if err := s.coordinator.SetDNSProxy(true); err != nil {
			return IPCResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to start DNS Proxy: %v", err),
			}
		}
		return IPCResponse{
			Success: true,
			Message: "DNS Proxy enabled",
		}

	case ActionDisableDNSProxy:
		if err := s.coordinator.SetDNSProxy(false); err != nil {
			return IPCResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to stop DNS Proxy: %v", err),
			}
		}
		return IPCResponse{
			Success: true,
			Message: "DNS Proxy disabled",
		}

	case ActionEnableAutoRefresh:
		s.coordinator.SetAutoRefresh(true)
		return IPCResponse{
			Success: true,
			Message: "Auto-refresh route enabled",
		}

	case ActionDisableAutoRefresh:
		s.coordinator.SetAutoRefresh(false)
		return IPCResponse{
			Success: true,
			Message: "Auto-refresh route disabled",
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
