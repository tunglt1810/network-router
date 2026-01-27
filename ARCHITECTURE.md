# Network Router - Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         macOS System                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────┐        ┌──────────────┐                       │
│  │  Menu Bar    │        │   Terminal   │                       │
│  │   (User)     │        │    (User)    │                       │
│  └──────┬───────┘        └──────┬───────┘                       │
│         │                       │                               │
│         v                       v                               │
│  ┌──────────────┐        ┌──────────────┐                       │
│  │  Tray App    │        │  CLI Client  │                       │
│  │ (User Mode)  │        │ (User Mode)  │                       │
│  │              │        │              │                       │
│  │ • Status     │        │ • status     │                       │
│  │ • Toggle     │        │ • enable     │                       │
│  │ • Apply      │        │ • disable    │                       │
│  │ • Clear      │        │ • apply      │                       │
│  │ • Quit       │        │ • clear      │                       │
│  └──────┬───────┘        └──────┬───────┘                       │
│         │                       │                               │
│         │    Unix Socket (0666) │                               │
│         └───────────┬───────────┘                               │
│                     │                                           │
│                     v                                           │
│           ┌──────────────────┐                                  │
│           │   IPC Server     │                                  │
│           │ /tmp/network-    │                                  │
│           │  router.sock     │                                  │
│           └────────┬─────────┘                                  │
│                    │                                            │
│  ┌─────────────────┴─────────────────────────────────┐          │
│  │          Network Router Daemon (Root)             │          │
│  │                                                   │          │
│  │  ┌──────────────┐  ┌───────────────┐              │          │
│  │  │   Monitor    │  │  Router Core  │              │          │
│  │  │              │  │               │              │          │
│  │  │ • Network    │  │ • Route Mgmt  │              │          │
│  │  │   Changes    │  │ • DNS Resolve │              │          │
│  │  │ • Interface  │  │ • CIDR Routes │              │          │
│  │  │   Detection  │  │               │              │          │
│  │  └──────┬───────┘  └───────┬───────┘              │          │
│  │         │                  │                      │          │
│  │         v                  v                      │          │
│  │    ┌────────────────────────────┐                 │          │
│  │    │      State Manager         │                 │          │
│  │    │  • auto_routing_enabled    │                 │          │
│  │    │  • routes_applied          │                 │          │
│  │    │  • last_applied_at         │                 │          │
│  │    └────────────────────────────┘                 │          │
│  └─────────────────┬─────────────────────────────────┘          │
│                    │                                            │
│                    v                                            │
│         ┌──────────────────────┐                                │
│         │   macOS Networking   │                                │
│         │                      │                                │
│         │  • route command     │                                │
│         │  • networksetup      │                                │
│         │  • DNS resolution    │                                │
│         └──────────────────────┘                                │
│                    │                                            │
│                    v                                            │
│         ┌──────────────────────┐                                │
│         │  Network Interfaces  │                                │
│         │                      │                                │
│         │  • Wi-Fi (Internal)  │                                │
│         │  • iPhone USB        │                                │
│         │    (Internet)        │                                │
│         └──────────────────────┘                                │
└─────────────────────────────────────────────────────────────────┘
```

## Component Details

### 1. Daemon (Root Process)
- **File**: `daemon/daemon.go`, `daemon/monitor.go`, `daemon/state.go`
- **Runs as**: root (via launchd)
- **Config**: `/usr/local/etc/network-router/config.yaml`
- **Responsibilities**:
  - Monitor network changes
  - Detect WiFi and Phone interfaces
  - Apply/clear routing rules
  - Resolve internal domains
  - Maintain routing state

### 2. IPC Server
- **File**: `daemon/ipc.go`
- **Socket**: `/tmp/network-router.sock` (permissions: 0666)
- **Protocol**: JSON over Unix socket
- **Commands**:
  - `status`: Get current state
  - `enable`: Enable auto-routing
  - `disable`: Disable auto-routing
  - `apply`: Force apply routes
  - `clear`: Clear all routes
  - `restart`: Clear + Apply

### 3. CLI Client
- **File**: `client/client.go`
- **Runs as**: User (no sudo required)
- **Usage**: `network-router <command>`
- **Output**: Terminal text

### 4. Tray App
- **File**: `tray/tray.go`
- **Runs as**: User
- **Library**: `github.com/getlantern/systray`
- **Features**:
  - Visual status indicator (icon color)
  - Menu-driven controls
  - Auto-refresh every 5s
  - No terminal required

### 5. Assets
- **File**: `assets/icon.go`
- **Purpose**: Generate tray icons dynamically
- **Icons**:
  - Green: Active and routing
  - Gray: Inactive or disabled
  - Red: Error/disconnected

### 6. Router Core
- **Files**: `pkg/core/router.go`, `pkg/utils/*.go`
- **Responsibilities**:
  - Execute route commands
  - Resolve domain to IPs
  - Handle CIDR ranges
  - Interface detection

## Data Flow

### Status Check Flow
```
User → Tray/CLI → IPC Socket → Daemon → State Manager → Response
                                               ↓
                                        Check network state
```

### Route Application Flow
```
User → Tray/CLI → IPC Socket → Daemon → Router Core → macOS route cmd
                                               ↓
                                        Update state
```

### Network Change Flow
```
macOS Network Change → Monitor → Router Core → Apply routes
                                        ↓
                                  Update state
```

## Security Notes

1. **Socket Permissions (0666)**: 
   - Allows any local user to communicate with daemon
   - Acceptable for single-user systems
   - For multi-user: use 0660 + group ownership

2. **Daemon runs as root**:
   - Required for route modifications
   - Sandboxed via launchd
   - No network exposure (Unix socket only)

3. **No authentication**:
   - Socket is local-only
   - Physical access required
   - Could add token-based auth if needed

## Performance

- **Daemon**: ~10MB memory, minimal CPU
- **Tray App**: ~5-10MB memory, polling every 5s
- **IPC Latency**: <10ms typical
- **Route Application**: ~100-500ms depending on entries

## Monitoring

### Logs
```bash
# Daemon logs
tail -f /var/log/network-router.log

# System logs
grep network-router /var/log/system.log
```

### Service Status
```bash
# Check if daemon is running
sudo launchctl list | grep network-router

# Check tray app
ps aux | grep "network-router tray"
```

### Network State
```bash
# Check routing table
netstat -rn

# Check interfaces
networksetup -listallhardwareports
```
