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
│  │  ┌──────────────────┐  ┌────────────────────┐    │          │
│  │  │ NetworkDetector  │  │      DNSProxy      │    │          │
│  │  │                  │  │                    │    │          │
│  │  │ • Poll every 5s  │  │ • udp+tcp loopback  │   │          │
│  │  │ • Interface      │  │ • force_ipv4 gate  │    │          │
│  │  │   discovery      │  │ • answer cache     │    │          │
│  │  └────────┬─────────┘  └──────────┬─────────┘    │          │
│  │           │ publish               │ resolve      │          │
│  │           v                       v              │          │
│  │  ┌──────────────────┐  ┌────────────────────┐    │          │
│  │  │  SnapshotStore   │  │  UpstreamResolver  │    │          │
│  │  │  (atomic ptr)    │←─┤  phone → pinned →  │    │          │
│  │  └────────┬─────────┘  │  degraded          │    │          │
│  │           │            └──────────┬─────────┘    │          │
│  │           v                       │ submit       │          │
│  │  ┌──────────────────┐             │              │          │
│  │  │   Coordinator    │             │              │          │
│  │  │ • auto_routing   │             │              │          │
│  │  │ • routes_applied │             │              │          │
│  │  └────────┬─────────┘             │              │          │
│  │           v                       v              │          │
│  │  ┌──────────────────┐  ┌────────────────────┐    │          │
│  │  │      Router      │─►│    RouteApplier    │    │          │
│  │  │ • route plan     │  │ • sole route owner │    │          │
│  │  │ • DNS resolve    │  │ • state.yaml       │    │          │
│  │  └──────────────────┘  └──────────┬─────────┘    │          │
│  └─────────────────────────────────── │ ────────────┘          │
│                                       │                        │
│                                       v                        │
│         ┌──────────────────────┐                                │
│         │   macOS Networking   │                                │
│         │                      │                                │
│         │  • route command     │                                │
│         │  • networksetup      │                                │
│         │  • ipconfig          │                                │
│         │  • /etc/resolver     │                                │
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
- **Files**: `daemon/daemon.go`, `daemon/coordinator.go`, `daemon/network_detector.go`
- **Runs as**: root (via launchd)
- **Config**: `/usr/local/etc/network-router/config.yaml`
- **State**: `/usr/local/var/network-router/state.yaml`
- **Responsibilities**:
  - Reconcile resolver files and routes left by a previous run, before anything else
  - Detect WiFi and Phone interfaces and publish the result
  - Apply and clear routing rules
  - Resolve configured domains
  - Own DNS proxy and refresh-schedule lifecycle

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
  - `refresh`: Re-resolve domains and re-apply routes synchronously

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
  - Auto-refresh status polling every 5s
  - Transient single-item popup notifications (auto-dismissing after 3s, no system notification / Script Editor dependency)
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
  - Build the route plan from configuration
  - Resolve non-wildcard domains to IPs. Wildcard patterns are excluded, since the
    base of `*.example.com` resolves to an address its subdomains need not use.
    Wildcards are handled at request time by the DNS proxy.
  - Refresh interface discovery immediately before applying
- **Never touches the default route.** Only host and network routes are installed.

### 7. RouteApplier
- **File**: `pkg/core/route_applier.go`
- **Role**: the single writer for every route the product installs and for the
  state file at `/usr/local/var/network-router/state.yaml`. The DNS request path
  only enqueues hosts, so answering a query never waits on the routing table.
- **Behaviour**:
  - Tracks applied destinations in a set, so repeat answers cost a lookup
  - Treats an already-present route as installed and records it for cleanup
  - Validates the gateway and the destination before running `route`
  - Persists debounced, writing atomically via temp file plus rename
  - `ReconcileFromState()` removes routes recorded by a previous run

### 8. DNS Proxy and UpstreamResolver
- **Files**: `pkg/core/dns_proxy.go`, `pkg/core/upstream.go`, `pkg/core/dnscache.go`
- **Listens**: `127.0.0.1:<dns_proxy_port>` on **both UDP and TCP**. The TCP
  listener is required because macOS retries over TCP when a reply is truncated.
- **Scope**: the proxy performs no domain matching. macOS forwards a query here
  only when a file in `/etc/resolver` covers the name, and such a file covers the
  entire subtree beneath it, so the resolver files are the complete scope
  definition and every query arriving here is in scope by construction.
- **Upstream order**:
  1. the DNS server the phone advertises over DHCP
  2. pinned public resolvers, each given a `/32` route over the phone
  3. the system resolver, degraded — the answer is returned but marked unroutable

  A configured domain is routed over the phone because Wi-Fi is not the desired
  path for it, so name resolution has to travel the same way. Candidates are
  raced with a 250 ms stagger, which keeps the common case at one query while
  bounding the cost of an unreachable candidate.
- **Fail-open**: `/etc/resolver/<domain>` makes this proxy the sole resolver for
  the names it covers, so a degraded answer leaves the client able to connect
  where a failure response would not.
- **IPv4 enforcement**: AAAA, HTTPS and SVCB are answered with an empty NOERROR
  when `force_ipv4` is on. Those types let a client reach a host without the A
  records that host routes are installed for.
- **Lifecycle**: listeners are bound and confirmed serving *before* any resolver
  file is written, so resolver files exist only while the port they name is being
  served. `Start`/`Stop` are mutex-guarded and idempotent. `ReconcileResolvers()`
  deletes generated resolver files recorded by an earlier process.

### 9. SnapshotStore
- **File**: `pkg/core/network.go`
- **Role**: `NetworkDetector` is the only writer; every other component reads the
  latest immutable `NetworkSnapshot` through an atomic pointer, so reported status
  matches the state routing decisions used.

## Data Flow

### Status Check Flow
```
User → Tray/CLI → IPC Socket → Coordinator → SnapshotStore + lifecycle flags
                                                      ↓
                                                  Response
```

### Route Application Flow
```
User → Tray/CLI → IPC Socket → Coordinator → Router → RouteApplier → route cmd
                                                            ↓
                                                    state.yaml (debounced)
```

### Network Change Flow
```
macOS Network Change → NetworkDetector → SnapshotStore (atomic)
                              ↓
                        Coordinator → Router → RouteApplier → route cmd
```

### DNS Request Flow
```
App → mDNSResponder → /etc/resolver/<domain> → 127.0.0.1:5454 (udp+tcp)
                                                      ↓
                              force_ipv4 gate (AAAA/HTTPS/SVCB → empty NOERROR)
                                                      ↓
                                                  DNS cache
                                                      ↓
                              UpstreamResolver: phone DNS → pinned → degraded
                                                      ↓
                              reply to client  ┄┄┄┄┄┄┄→ RouteApplier.Submit (async)
```
The reply is not gated on the routing table. The applier reads the phone gateway
from the SnapshotStore, so no subprocess runs on the request path.

### Startup Reconciliation Flow
```
daemon start → ReconcileResolvers()         (delete generated /etc/resolver files)
             → applier.ReconcileFromState() (delete routes recorded by a prior run)
             → normal operation
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
