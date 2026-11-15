package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
)

const SocketPath = "/tmp/network-router.sock"

// IPC Action Constants
const (
	ActionStatus  = "status"
	ActionOn      = "on"
	ActionOff     = "off"
	ActionRefresh = "refresh"
)

// IPCRequest represents a client request
type IPCRequest struct {
	Action string `json:"action"`
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
	os.Remove(SocketPath)

	listener, err := net.Listen("unix", SocketPath)
	if err != nil {
		return fmt.Errorf("failed to create Unix socket: %w", err)
	}
	s.listener = listener

	// Set permissions to allow user-mode apps (like tray) to connect
	if err := os.Chmod(SocketPath, 0666); err != nil {
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	log.Printf("IPC server listening on %s", SocketPath)

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
	os.Remove(SocketPath)
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

	case ActionOn:
		applied, err := s.coordinator.On()
		if err != nil {
			return IPCResponse{Success: false, Message: fmt.Sprintf("Failed to apply routes: %v", err)}
		}
		if !applied {
			return IPCResponse{Success: true, Message: "Routing is on. The daemon applies routes when tethering is ready."}
		}
		return IPCResponse{Success: true, Message: "Routing is on. Routes are applied."}

	case ActionOff:
		if err := s.coordinator.Off(); err != nil {
			return IPCResponse{Success: false, Message: fmt.Sprintf("Routing is off, but clearing routes failed: %v", err)}
		}
		return IPCResponse{Success: true, Message: "Routing is off. Routes are cleared."}

	case ActionRefresh:
		if err := s.coordinator.Refresh(); err != nil {
			return IPCResponse{Success: false, Message: fmt.Sprintf("Failed to refresh routes: %v", err)}
		}
		return IPCResponse{Success: true, Message: "Routes refreshed"}

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
