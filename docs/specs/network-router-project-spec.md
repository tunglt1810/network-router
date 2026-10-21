# Network Router Project Specification

## 1. Document status

- **Status:** Source-derived specification
- **Language:** English
- **Scope:** Current implementation in this repository
- **Primary platform:** macOS
- **Entry point:** `main.go`

This document describes the behavior implemented by the Go source, service definitions, and installation scripts. Where the README or architecture document differs from the source, the source behavior is authoritative.

## 2. Product purpose

Network Router is a macOS system daemon that manages split routing across two active network connections:

- Wi-Fi is treated as the internal/corporate path.
- Phone USB tethering is treated as the selected path for configured external destinations.

The product provides:

- Automatic route application and cleanup when network interfaces change.
- Static CIDR routing through the phone connection.
- Domain-to-IPv4 resolution followed by host-route installation through the phone connection.
- A local DNS proxy that resolves the configured domains over the tethered path and installs host routes for the answers it returns.
- A root daemon controlled by a user-mode CLI client or menu-bar tray application.
- Optional cron-based route refresh.

### 2.1 Governing principle

The DNS path and the data path use the same network.

A configured domain is routed over the tethering connection because the Wi-Fi network is not the desired path for it, and frequently cannot reach or resolve it at all. Name resolution for such a domain must therefore also travel over the tethering connection. Resolving over one network while routing over another produces answers that the routed path may not be able to use, and fails outright whenever the resolving network cannot answer for the name.

## 3. Runtime architecture

```text
macOS network interfaces
          |
          v
NetworkDetector --> SnapshotStore (atomic, single writer)
          |               ^          ^
          | snapshot      |          |
          v               |          |
      Coordinator --> Router --> RouteApplier --> macOS route command
          |                          ^
          | start/stop               | Submit (async, non-blocking)
          v                          |
      DNSProxy --> UpstreamResolver --+
          ^              (phone DNS -> pinned -> degraded)
          |
CLI client / Tray app -- JSON over Unix socket --> IPCServer
```

Two invariants shape this layout:

- `SnapshotStore` has exactly one writer, the network detector. Every other component reads an immutable snapshot. No component performs interface discovery on the DNS request path.
- `RouteApplier` is the only writer of the routing table and of the route state file. The DNS proxy submits hosts to it asynchronously and never waits for a `route` invocation to complete.

### 3.1 Components

| Component | Source | Responsibility | Execution context |
| --- | --- | --- | --- |
| CLI entry point | `main.go` | Dispatches daemon, tray, and control commands | User or root, depending on command |
| Daemon | `daemon/daemon.go` | Loads config, reconciles prior state, and starts coordinated services | Root under launchd |
| Coordinator | `daemon/coordinator.go` | Owns routing lifecycle state and serializes network, IPC, DNS, and refresh actions | Daemon process |
| Network detector | `daemon/network_detector.go` | Polls interface state, publishes `NetworkSnapshot`, and notifies the coordinator | Daemon goroutine |
| Snapshot store | `pkg/core/network.go` | Publishes the latest immutable discovery result through an atomic pointer | Daemon process |
| Router | `pkg/core/router.go` | Builds the route plan from configuration and resolves non-wildcard domains | Daemon process |
| Route applier | `pkg/core/route_applier.go` | Sole owner of installed routes and of the state file; validates, deduplicates, persists, and reconciles | Daemon process and one worker goroutine |
| Route manager | `pkg/core/route_manager.go`, `pkg/core/os_route_manager.go` | Abstracts host route operations and provides a unit-test seam | Daemon process |
| DNS proxy | `pkg/core/dns_proxy.go` | Serves UDP and TCP on loopback, manages resolver files, enforces IPv4, and submits resolved hosts for routing | Daemon goroutine |
| Upstream resolver | `pkg/core/upstream.go` | Selects and races upstreams over the tethered path; reports whether an answer is routable | Daemon process |
| DNS cache | `pkg/core/dnscache.go` | TTL and LRU cache for upstream answers | Daemon process |
| IPC server | `daemon/ipc.go` | Serves local JSON requests over a Unix socket | Daemon goroutine |
| CLI client | `client/client.go` | Sends IPC requests and renders terminal output | User process |
| Tray app | `tray/tray.go` | Displays status and invokes IPC actions from a menu bar icon | User process |
| Log manager | `daemon/log_manager.go` | Truncates the daemon log on a daily cron schedule | Daemon process |
| Installation scripts | `install_service.sh`, `uninstall_service.sh` | Build, install, load, unload, and remove launchd services | Root shell |

## 4. Platform and privilege requirements

The implementation depends on macOS-specific commands and launchd behavior:

- `networksetup -listallhardwareports` for hardware-port discovery.
- `ifconfig` for interface address inspection.
- `ipconfig getoption <device> router` for gateway discovery.
- `ipconfig getpacket <device>` and `ipconfig getoption <device> domain_name_server` for the resolver advertised on the tethered interface.
- `route` for routing table changes.
- `/etc/resolver` for per-domain DNS resolver files.
- `/usr/local/var/network-router/state.yaml` for the record of installed routes.
- `launchctl` for daemon and tray lifecycle management.
- `osascript` for the tray debug action.

The daemon is installed as a system LaunchDaemon and requires root privileges to modify routes and manage `/etc/resolver`. The CLI and tray run as the logged-in user.

Route commands are invoked directly rather than through `sudo`. The daemon already holds root, so `sudo` would add a process spawn and a dependency on the effective user's sudo configuration without granting any additional privilege.

## 5. Configuration contract

Configuration is YAML loaded by `core.LoadConfig`. Unknown YAML fields are ignored by the decoder. The supported fields are:

| YAML key | Type | Runtime meaning |
| --- | --- | --- |
| `tether_domains` | list of strings | Domains whose resolved IPv4 addresses are routed through the phone path. A leading `*.` is supported. |
| `tether_cidrs` | list of strings | CIDR destinations routed through the phone path without DNS resolution. |
| `wifi_interface_name` | string | Case-insensitive service/hardware-port keyword used to find Wi-Fi. It is not a device number. Empty value defaults to `Wi-Fi`. |
| `phone_interface_name` | string | Case-insensitive service/hardware-port keyword used to find tethering. It is not a device number such as `en7` or `en8`. Empty value defaults to `iPhone USB`. |
| `route_refresh_cron` | string | Cron expression used for scheduled refreshes. Empty value disables scheduling. |
| `auto_refresh_route` | boolean | Initial state of scheduled route refresh. |
| `dns_proxy_enabled` | boolean | Initial DNS-proxy startup preference. Runtime status becomes true only after the proxy has started. |
| `dns_proxy_port` | integer | Local listener port, bound on both UDP and TCP. `0` defaults to `5454`. |
| `dns_upstream` | string | Optional single upstream address that overrides upstream selection entirely. A missing port defaults to `53`. |
| `dns_upstream_pinned` | list of strings | Fallback resolvers reached over the tethered path. Defaults to `1.1.1.1:53` and `8.8.8.8:53`. Each address receives a `/32` route through the phone gateway. |
| `force_ipv4` | boolean | Answer `AAAA`, `HTTPS`, and `SVCB` with an empty `NOERROR` so clients use the routed `A` records. Defaults to `true` when the key is absent. |
| `state_path` | string | Overrides the route state file location. Defaults to `/usr/local/var/network-router/state.yaml`. |

The installer copies `config.yaml` into `/usr/local/etc/network-router/config.yaml` and backs up an existing installed config before replacing it.

macOS must expose the selected phone service with a usable non-link-local IPv4 address and a gateway. The application does not select arbitrary active USB, Ethernet, or VPN interfaces, and it cannot repair a macOS tethering service that has not negotiated an address.

### 5.1 Domain entry rules

- Domain entries are lowercased and deduplicated.
- Wildcard entries such as `*.example.com` are excluded from batch resolution. The base name resolves to an address that subdomains do not necessarily use, so a host route derived from it would not correspond to the traffic being routed. Wildcards are handled at request time by the DNS proxy, which observes the name actually queried.
- Batch resolution uses the Go system resolver, requests IPv4 records only, and runs with a concurrency limit of eight.
- Each domain is attempted twice with a 500 ms backoff between failed attempts.
- A partial batch is accepted. Unresolved domains are reported and skipped.
- Entries that are literal IP addresses are excluded from resolver-file generation and logged. A file in `/etc/resolver` named after an IP address has no effect; such destinations belong in `tether_cidrs`.
- A resolver file covers the entire subtree beneath its name, so `example.com` and `*.example.com` are equivalent for resolver purposes and produce a single file.

## 6. Network detection and automatic state machine

`NetworkDetector` polls every five seconds. Each cycle it publishes the snapshot to `SnapshotStore` and delivers it through a one-element buffered channel; a newer snapshot replaces a queued older one. The same discovery implementation serves the detector and the router, so a device-number change is interpreted identically by every component.

Network facts are held in one place. Status reporting reads the published snapshot, so reported state matches the state that routing decisions used. The coordinator retains only the previous phone device and gateway, which is what is needed to detect a path change.

Wi-Fi is considered active when it has a usable IPv4 address. The phone path is considered ready only when its matching service has a usable non-link-local IPv4 address and a gateway that parses and is neither unspecified, loopback, nor multicast. Interface discovery matches service/hardware-port names case-insensitively using configured keywords plus fallback keywords:

- Wi-Fi: configured keyword and `Wi-Fi`.
- Phone: configured keyword, `iPhone USB`, `iPad USB`, and `RNDIS`.

The selected device name is discovered at runtime. The implementation does not persist or hard-code `en7`, `en8`, or any other macOS device number. A candidate with only a `169.254.x.x` address, no gateway, or an unusable gateway is reported as not ready together with the reason.

The coordinator starts with `auto_routing_enabled = true`, `routes_applied = false`, and runtime DNS status disabled. The DNS and refresh configuration controls whether those services may start after a successful route apply.

Route operations are serialized. Network events, `apply`, `clear`, and `refresh` hold one operation lock for their full duration, so an IPC command never interleaves with a network event and installed routes always match `routes_applied`. `status` does not take this lock and answers while an operation runs.

`apply` is a request for routing, not a one-shot route install. It always leaves auto-routing enabled, so the `disable` rule above cannot remove routes that `apply` installed. An `apply` that fails while the path is ready also leaves auto-routing enabled, and the next network event retries.

When `apply` installs routes while none were applied, the coordinator records the phone device and gateway it used, so the next network event does not report a false path change. When routes were already applied, the recorded path is kept, so a real gateway change is still detected and rebound.

| Event or action | Coordinator behavior |
| --- | --- |
| Wi-Fi and phone become ready while routes are not applied | Rediscover, resolve targets, add pinned-resolver routes, CIDR routes, and host routes, persist state, mark routes applied, then start the configured DNS proxy and refresh cron. |
| Either interface becomes unusable while routes are applied | Stop the DNS proxy, then clear routes and mark routes not applied. The proxy is stopped first so its resolver files are removed and the system returns to its normal resolver promptly. |
| Phone device or gateway changes while routes are applied | Stop services, clear the old routes, rediscover the current phone path, and apply routes through the new device and gateway. |
| Network discovery fails | Publish a not-ready snapshot carrying the failure reason, so active routes are not retained silently. |
| Auto-routing is disabled while routes are applied | Clear existing routes on the next network event. |
| `enable` | Sets the auto-routing flag to true. It does not immediately apply routes. |
| `disable` | Sets the auto-routing flag to false. Existing routes are cleared when the next network event is processed. |
| `apply` | Sets the auto-routing flag to true. When the published snapshot shows a ready Wi-Fi and phone path, applies routes immediately and starts DNS and refresh services. Otherwise, or when fresh discovery during apply reports the phone not ready, applies nothing, returns success with a waiting message, and lets the next ready snapshot apply the routes. |
| `clear` | Clears routes and, if auto-routing was enabled, disables auto-routing to prevent immediate re-application. |
| `restart` | Performs `clear` followed by `apply`. Because `apply` re-enables auto-routing, the routes survive the next network event. |
| `refresh` | Synchronously re-resolves domains and reapplies routes, returning success or failure details. |

## 7. Route application and cleanup

### 7.1 Apply flow

1. Rediscover both services and require a ready phone interface.
2. Add a `/32` route for each pinned upstream resolver through the phone gateway, classified as `infrastructure`. These routes are what make the proxy's fallback resolvers reachable over the tethered path rather than over the default route.
3. Add every configured CIDR through the current phone gateway, classified as `static`.
4. Resolve non-wildcard domains and add each resolved IPv4 address as a `/32` host route, classified as `static`.
5. Record every installed destination and its classification in the state file.

Destinations are validated before installation. Unspecified, loopback, multicast, and link-local targets are refused. Private ranges are permitted, because the configuration legitimately contains internal domains.

A destination already present in the routing table is treated as installed and is recorded for cleanup, so it participates in the normal lifecycle rather than blocking it.

Individual route failures are logged and skipped. Apply fails only when no route at all could be installed, in which case the coordinator does not mark `routes_applied` true.

The default system gateway is not changed during apply. The implementation adds specific destinations only.

### 7.2 Clear flow

1. Delete every destination in the applier's applied set.
2. Remove the state file.

Deletion failures are logged per destination. A destination that is no longer in the table is treated as successfully removed, since deletion is idempotent.

The default route is not modified. Apply does not change it, so cleanup has nothing to restore.

### 7.3 Startup reconciliation

Before normal operation the daemon:

1. Deletes every file in `/etc/resolver` containing the `# Generated by Network Router` marker. Such a file surviving from a terminated process directs queries to a port that is not being served, which prevents the affected domains from resolving at all.
2. Deletes every route recorded in the state file, then removes the file. Such a route points at a gateway that may no longer exist, which blackholes the destinations the product is meant to keep reachable.

The state file lives at a fixed path rather than under `os.TempDir()`, because `TMPDIR` differs between a root LaunchDaemon and an interactive `sudo` shell and the record must be readable by any later run.

## 8. DNS proxy specification

### 8.1 Listener lifecycle

When enabled, `DNSProxy` binds `127.0.0.1:<port>` on both UDP and TCP. The default port is `5454`. The TCP listener is required because macOS retries over TCP when a reply is truncated.

Start order:

1. Bind both sockets. A bind failure is returned to the caller.
2. Serve on each and wait for the listener to report that it is running.
3. Write resolver files.

If resolver setup fails, the listeners are shut down. Resolver files therefore exist only while the port they name is being served.

`Start` and `Stop` are mutex-guarded and idempotent. `Stop` removes resolver files before shutting the listeners down, so the system returns to its normal resolver promptly, and purges the answer cache.

Resolver file names are derived by lowercasing each configured domain and stripping a leading `*.`, then deduplicating the result.

Without root the proxy serves on loopback but skips resolver setup and logs that split DNS is inactive. In that state it receives no queries.

### 8.2 Upstream selection

Preference order:

1. The DNS server advertised by DHCP on the tethered interface. It is reachable through the phone's directly connected subnet route and needs no additional route.
2. The pinned public resolvers, each of which has a `/32` route through the phone gateway installed by the applier.
3. The system resolver, as a degraded last resort. Its answer is returned but marked **not routable**, so no route is derived from an address supplied by a network other than the tethered one.

Candidates in the tethered set are queried with a staggered race: the first fires immediately and each subsequent candidate 250 ms later, the first definitive answer wins, and the remainder are cancelled. In the common case this costs a single upstream query, because the phone resolver answers before the next candidate is due. The stagger bounds the cost of an unreachable candidate at 250 ms rather than at a full attempt timeout, which matters because a pinned resolver is unreachable until its host route exists.

The total budget is 3.5 s, under the 5 s timeout macOS applies to a resolver listed in `/etc/resolver`. Each individual attempt is capped at 1.5 s.

`dns_upstream`, when set, replaces the whole order and is used alone.

Only `NOERROR` and `NXDOMAIN` are definitive. `SERVFAIL`, `REFUSED`, and `NOTIMP` are treated as upstream failures and advance to the next candidate. A truncated reply is retried over TCP against the same upstream.

The degraded fallback exists because `/etc/resolver/<domain>` makes this proxy the sole resolver for the names it covers. A returned answer, even one whose addresses are not routed, leaves the client able to connect; a failure response does not.

### 8.3 Request handling

1. A request with no question is answered `FORMERR`.
2. When `force_ipv4` is enabled and the query type is `AAAA`, `HTTPS`, or `SVCB`, the reply is an empty `NOERROR`. These types allow a client to reach a host without the `A` records that host routes are installed for, so answering them would let traffic leave over the default route.
3. A live cache entry is served directly. TTLs are aged by the time spent in cache.
4. Otherwise the query is resolved through `UpstreamResolver`.
5. When the answer is routable, its `A` records are submitted to `RouteApplier`. Submission is asynchronous and the reply is not gated on it.
6. Definitive answers are cached. `SERVFAIL` is returned only when no upstream produced an answer.

The proxy performs no domain matching. macOS forwards a query to it only when a file in `/etc/resolver` covers the name, and such a file covers the entire subtree beneath it. The resolver files are therefore the complete and authoritative scope definition, and every query reaching the proxy is in scope by construction.

### 8.4 Answer cache

Entries are keyed by name, query type, and query class. Positive TTLs are taken from the minimum record TTL in the answer and authority sections, clamped to between 5 s and 1 h. `NXDOMAIN` is cached for 30 s. Failure responses and truncated replies are not cached. Capacity is bounded and eviction is least-recently-used. A cached reply is returned with the requesting message's ID and question.

## 9. Refresh behavior

When `route_refresh_cron` is non-empty and auto-refresh is enabled, the coordinator starts a cron scheduler after routes are applied. A scheduled refresh:

1. Stops the DNS proxy and the refresh schedule.
2. Clears the installed routes.
3. Resets coordinator route state.
4. Rediscovers interfaces and reapplies routes.
5. Restarts the refresh schedule and DNS proxy after successful reapplication.

Routes are absent and the proxy is down for the duration of the reapply. The length of that window is bounded by batch resolution, which excludes wildcard base domains and runs with a concurrency limit of eight.

The tray and IPC layer can enable or disable auto-refresh at runtime. The setting is held in memory and is not persisted back to YAML.

## 10. IPC contract

The daemon listens on `/tmp/network-router.sock` using newline-terminated JSON values encoded by Go's JSON encoder. The socket is created with mode `0666` so user-mode clients can connect.

### 10.1 Request and response shapes

```json
{"action":"status"}
```

```json
{
  "success": true,
  "message": "optional human-readable result",
  "data": {
    "auto_routing_enabled": true,
    "routes_applied": false,
    "wifi_active": true,
    "phone_active": false,
    "wifi_device": "en0",
    "phone_device": "en7",
    "wifi_gateway": "10.0.0.1",
    "phone_gateway": "",
    "phone_ready": false,
    "phone_status_reason": "phone interface en7 has no usable IPv4 address",
    "active_routes": 0,
    "last_applied_at": "0001-01-01T00:00:00Z",
    "last_cleared_at": "0001-01-01T00:00:00Z",
    "dns_proxy_enabled": true,
    "auto_refresh_enabled": false
  }
}
```

`params` is accepted on requests but no current action consumes parameters.

| Action | Result |
| --- | --- |
| `status` | Returns the coordinator status object, including selected devices, gateways, phone readiness, a readiness reason, and the number of installed routes. |
| `enable` / `disable` | Changes the auto-routing flag. |
| `apply` | Enables auto-routing. Synchronously applies routes when the path is ready and returns failure details if setup fails. When the path is not ready, returns success with the message `Auto-routing is on. The daemon applies routes when tethering is ready.` |
| `clear` | Synchronously clears routes and reports whether auto-routing was disabled. |
| `restart` | Synchronously clears and reapplies routes. Returns the same waiting message as `apply` when the path is not ready. |
| `refresh` | Synchronously re-resolves domains, reapplies routes, and returns the result. |
| `enable_dns_proxy` / `disable_dns_proxy` | Starts or stops the DNS proxy. |
| `enable_auto_refresh` / `disable_auto_refresh` | Changes scheduled refresh state. |
| Any other action | Returns `success: false` and an unknown-action message. |

The client uses a five-second default connection and operation timeout. `apply`, `clear`, `refresh`, and `restart` use a 120-second timeout, because route operations wait behind each other in the daemon and an apply can spend most of that time resolving domains.

## 11. CLI contract

The executable dispatches these commands:

| Command | Behavior |
| --- | --- |
| `daemon [-config path]` | Loads the YAML file and runs the daemon until cancellation. Default config path is `config.yaml`. |
| `tray` | Starts the menu-bar application in the current user session. |
| `status` | Prints daemon, interface, route-count, DNS proxy, and auto-refresh state. |
| `enable`, `disable` | Toggles automatic routing. |
| `apply`, `clear`, `restart` | Performs route operations. |
| `enable-dns`, `disable-dns` | Toggles the DNS proxy. |
| `refresh` | Synchronously re-resolves domains and reapplies routes. |
| `enable-auto-refresh`, `disable-auto-refresh` | Toggles the scheduled refresh. |
| `tray-enable`, `tray-disable` | Registers and starts, or stops and removes, the per-user LaunchAgent. |
| `help`, `-h`, `--help` | Prints usage. |

Every IPC action has a corresponding CLI command. An unrecognized command prints usage and exits non-zero.

## 12. Tray application contract

The tray polls `status` every five seconds. It displays:

- Green icon when auto-routing is enabled and routes are applied.
- Gray icon when connected but inactive or disabled, or before an active route state exists.
- Red icon when the daemon cannot be reached or returns an invalid status.

The status tooltip reports Wi-Fi and phone activity, and appends the phone readiness reason when one is present.

Available menu operations are status, auto-routing toggle, apply, refresh, clear, DNS-proxy toggle, auto-refresh toggle, debug log tail, hide/show icon, and quit. Menu actions call the corresponding IPC action and schedule a status update approximately 500 ms later.

The apply action shows the daemon's response message, and shows it as an error when the response reports `success: false`. After an apply that is waiting for tethering, the icon stays gray until routes are applied.

The debug action opens Terminal through `osascript` and runs `tail -f /var/log/network-router.log`.

Hiding the icon replaces it with a transparent pixel. The menu bar slot remains present and clickable, which is how the menu stays reachable to show the icon again.

Menu actions display a transient notification popup from the tray icon and mirror the same text to the tray log as `[<title>] <message>`. The popup does not use macOS system notifications, `osascript`, or Script Editor.

Popup behavior (`tray/popup_darwin.go`):

- The popup is a temporary menu with one disabled item that reads `<title>: <message>`. It shows the result only and accepts no action.
- It opens below the tray icon by temporarily replacing the status item's menu and clicking the status item button.
- It closes after 3 seconds. The timer is attached to the common and event-tracking run loop modes, so it fires while the menu is open.
- A click outside the popup closes it immediately.
- When the popup closes, the full tray menu is restored.
- A new popup closes a popup that is still open.

On platforms other than macOS, `tray/popup_other.go` provides a no-op popup. Only the log line is written.

## 13. Installation and service lifecycle

`make install` delegates to `install_service.sh`. The installer:

1. Requires root.
2. Builds `build/network-router` if it does not already exist.
3. Installs the binary at `/usr/local/bin/network-router`.
4. Installs and backs up the YAML config at `/usr/local/etc/network-router/config.yaml`.
5. Creates the state directory `/usr/local/var/network-router` with mode `0700` owned by `root:wheel`.
6. Installs the daemon LaunchDaemon at `/Library/LaunchDaemons/com.bez.network-router.plist`.
7. Installs the tray plist template at `/usr/local/etc/network-router/tray-agent.plist`.
8. Bootstraps and starts the daemon.
9. Leaves the tray opt-in; users enable it with `network-router tray-enable`.

The daemon plist runs at load, keeps the daemon alive, supplies a macOS command-search `PATH`, and writes stdout and stderr to `/var/log/network-router.log`.

`network-router tray-enable` copies the installed tray template to the user's `~/Library/LaunchAgents`, bootstraps it into `gui/<uid>`, and kickstarts it. `tray-disable` boots it out and removes the per-user plist.

`uninstall_service.sh` clears routes, stops both launchd services, removes generated resolver files marked with `# Generated by Network Router`, removes installed binaries, configuration, state directory, socket, and tray log files under its managed paths, and leaves `/var/log/network-router.log` for manual deletion.

## 14. Logging and persistence

- Daemon stdout and stderr: `/var/log/network-router.log` under the installed LaunchDaemon.
- Tray stdout and stderr: `/tmp/network-router-tray.log` and `/tmp/network-router-tray.err` under the tray plist.
- IPC socket: `/tmp/network-router.sock`.
- Installed route state: `/usr/local/var/network-router/state.yaml`, mode `0600` inside a `0700` directory, overridable with `state_path`.
- Log cleanup: `/var/log/network-router.log` is truncated daily at 10:00 by `LogManager`.

The state file records each installed destination together with its classification (`static`, `dynamic`, or `infrastructure`). It is written atomically through a temporary file and a rename, and writes are debounced by two seconds. A file left by a daemon that stopped unexpectedly is consumed by startup reconciliation described in section 7.3.

## 15. Security and operational constraints

- The daemon has root-level route and filesystem privileges.
- The IPC socket is world-readable and world-writable (`0666`) with no authentication or action authorization.
- Any local user able to reach the socket can request route changes, DNS-proxy changes, or route cleanup.
- The product assumes a single-user macOS workstation. A multi-user deployment would require socket ownership and mode restrictions plus authentication and authorization.
- DNS proxy resolver files are generated under `/etc/resolver`. Operators should avoid manually managed files with the same names as configured domains.
- Configuration changes are not hot-reloaded. Restart the daemon to load YAML changes.
- Domain routing reflects the DNS answers observed so far and may become stale until a refresh or a new proxy answer adds a route.
- Only IPv4 is routed. `force_ipv4` answers `AAAA`, `HTTPS`, and `SVCB` empty for the domains in scope so that IPv6 and address hints do not bypass the tethered path. Disabling it re-opens that bypass. Routing IPv6 over the tethered path is not implemented.
- Pinning public resolvers to the tethered path sends DNS queries over cellular data.
- The first connection to a newly resolved address may leave over the default route, because the DNS reply is not gated on the routing table. The window is bounded by a set lookup and one `route` invocation.
- Because the proxy is the sole resolver for the configured domains, its availability determines the availability of those domains. Section 8.2 describes the degraded fallback that limits the consequence of an upstream failure.

## 16. Test and verification requirements

The automated test suite covers network discovery, routing, DNS behavior, and coordinator transitions through injected seams:

- `pkg/utils/network_test.go`: hardware-port parsing, device remapping, IPv4 parsing, and link-local rejection.
- `pkg/core/network_test.go`: service-aware phone selection and rejection of unknown active interfaces.
- `pkg/core/router_test.go`: gateway selection after device remap, pinned-resolver routes, wildcard base-domain exclusion, fail-closed apply, tolerance of pre-existing routes, and cleanup.
- `pkg/core/route_applier_test.go`: destination normalization, gateway and target validation, idempotency, cleanup, startup reconciliation, atomic state writes, and concurrent submission.
- `pkg/core/upstream_test.go`: upstream preference order, failover on `SERVFAIL`, `NXDOMAIN` as definitive, TCP retry after truncation, the explicit override, and the guarantee that a non-tethered answer is never reported routable.
- `pkg/core/dnscache_test.go`: TTL clamping and aging, expiry, negative caching, query-type separation, LRU eviction, and refusal to cache failures or truncated replies.
- `pkg/core/dns_proxy_test.go`: resolver-name deduplication, IP-literal rejection, idempotent start and stop, the guarantee that no resolver file is written when a listener fails to bind, selective reconciliation, the `force_ipv4` gate, cache reuse, and route submission.
- `daemon/coordinator_test.go`: phone loss, gateway rebinding, service restart, failed apply state, status derivation from the snapshot store, apply after clear surviving the next network event, apply waiting when no snapshot exists or the phone is not ready, a second apply not hiding a gateway change, `disable` still clearing routes, and concurrent operations under the race detector.
- `client/client_test.go`: per-action request timeouts.
- `daemon/network_detector_test.go`: fail-closed snapshots and publication to the store when OS discovery fails.

Minimum verification for changes to routing or DNS behavior:

1. `go vet ./...` and `go test -race -count=1 ./...` on a macOS-capable development environment. The race detector is required, because route state is reachable from DNS handler goroutines.
2. Route-manager tests using a mock implementation, without modifying the host routing table.
3. Manual daemon status, apply, and clear checks with representative Wi-Fi and tethering interfaces.
4. A live check that a domain the Wi-Fi resolver cannot resolve is answered through the proxy:

   ```bash
   dig @127.0.0.1 -p 5454 <configured-domain> A
   ```

5. Confirmation that no generated resolver file survives a `kill -9` of the daemon followed by a restart.

## 17. Source map

- `main.go`: executable commands and tray launchd controls.
- `daemon/daemon.go`: daemon composition, startup reconciliation, goroutine lifecycle, and signal handling.
- `daemon/coordinator.go`: state machine, route lifecycle, DNS lifecycle, and refresh scheduling.
- `daemon/network_detector.go`: five-second interface polling, snapshot publication, and event emission.
- `daemon/ipc.go`: Unix-socket protocol and action dispatch.
- `daemon/log_manager.go`: daily log truncation.
- `pkg/core/config.go`: YAML configuration model and defaults.
- `pkg/core/network.go`: service-aware network discovery, readiness model, and snapshot store.
- `pkg/core/router.go`: route planning, non-wildcard domain resolution, apply, and clear.
- `pkg/core/route_applier.go`: installed-route ownership, validation, deduplication, atomic state persistence, and startup reconciliation.
- `pkg/core/dns_proxy.go`: UDP and TCP listeners, resolver-file lifecycle, IPv4 enforcement, and route submission.
- `pkg/core/upstream.go`: tethered-path upstream selection, staggered race, rcode handling, and TCP retry.
- `pkg/core/dnscache.go`: TTL and LRU answer cache.
- `pkg/core/route_manager.go`: route-operation abstraction, limited to add-via-gateway and delete.
- `pkg/core/os_route_manager.go`: production route-manager adapter.
- `pkg/utils/*.go`: macOS interface, gateway, DNS, and route command adapters.
- `client/client.go`: IPC client and terminal output.
- `tray/tray.go`: menu-bar UI and status polling.
- `tray/popup_darwin.go`, `tray/popup_other.go`: transient notification popup and its non-macOS no-op.
- `install_service.sh`, `uninstall_service.sh`, and `*.plist`: macOS service lifecycle.
