# Network Router Daemon

A service that automatically manages network routing on macOS, optimizing connections when using Wifi (for internal/corporate network) and USB Tethering/Ethernet (for high-speed Internet) simultaneously.

This project has evolved from a simple CLI tool into a **System Daemon + CLI Client + System Tray** architecture, allowing it to run in the background automatically and stably without manual intervention on every system boot.

## ⚡ Quick Start - Install with one command

```bash
git clone https://github.com/tunglt1810/network-router.git && cd network-router && sudo make install
```

Once installed, the daemon will run in the background. You can enable the tray icon anytime!
```bash
network-router tray-enable
```

## Features

*   **Background Daemon**: Runs as a system service (root), automatically starts with macOS.
*   **System Tray Icon**: Graphical interface on the menu bar for easy control, no terminal required.
*   **Auto-Switching**: Automatically detects when you plug/unplug iPhone USB Tethering or connect to Wifi to adjust the Routing Table immediately.
*   **Split Tunneling**: Destinations listed in `tether_domains` and `tether_cidrs` get host and network routes over the tethering connection. Everything else keeps using the system default route, untouched.
*   **CLI Client**: Command-line interface to check status or toggle services easily.
*   **DNS Proxy**: Wildcard domain routing by intercepting DNS requests. Queries for tethered domains are resolved **over the tethered path** so that name resolution and traffic use the same network. Falls back to the system resolver rather than failing the query, and enforces IPv4 so traffic stays on the routed address family.
*   **Auto Refresh**: Automatically updates domain IPs on a schedule (Cron).
*   **Auto-disable on Clear**: Automatically disables auto-routing when routes are cleared to prevent unintended re-application.

## Architecture

*   **Daemon (`network-router daemon`)**:
    *   Runs with **root** privileges.
    *   **NetworkDetector**: Polls for interface changes and publishes an immutable snapshot that every other component reads. The DNS request path performs no interface discovery of its own.
    *   **StateCoordinator**: Central state machine that manages auto-routing rules and DNS Proxy lifecycle.
    *   **RouteApplier**: Sole owner of the installed routes and of the state file recording them at `/usr/local/var/network-router/state.yaml`, which lets a later run clean up after an unexpected shutdown.
    *   **RouteManager**: Abstraction layer executing `route` commands.
    *   Opens an IPC socket at `/tmp/network-router.sock` to receive control commands.
*   **Tray App (`network-router tray`)**:
    *   Runs with **user** privileges.
    *   Displays an icon in the menu bar.
    *   Communicates with the Daemon via Unix socket.
    *   Control Menu: Status, Auto-Routing toggle, Apply, Refresh, Clear, DNS Proxy toggle, Auto Refresh toggle, Debug, Hide Icon, Quit.
*   **Client (`network-router <cmd>`)**:
    *   User-executed commands to communicate with the Daemon via socket.

## 📦 Installation

### Method 1: Automatic Installation (Recommended)

Just run a single command:

```bash
git clone https://github.com/tunglt1810/network-router.git && cd network-router && sudo make install
```

The script will automatically:
- ✅ Build binary from source code
- ✅ Install to `/usr/local/bin/network-router`
- ✅ Install config file to `/usr/local/etc/network-router/`
- ✅ Create the state directory `/usr/local/var/network-router/`
- ✅ Register and start daemon service (LaunchDaemon)
- ✅ Install tray app configuration (LaunchAgent, opt-in)
- ✅ Run health check to verify service

### Method 2: Manual Installation (Step-by-step)

If you want more detailed control:

```bash
# Clone repository
git clone https://github.com/tunglt1810/network-router.git
cd network-router

# Build binary
make build

# Install service (requires sudo password)
sudo ./install_service.sh
```

The installation script will:
1.  Build the `network-router` binary and copy it to `/usr/local/bin/`.
2.  Create a configuration directory at `/usr/local/etc/network-router/`.
3.  Create the state directory `/usr/local/var/network-router/`, where installed routes are recorded.
4.  Install the daemon service into `/Library/LaunchDaemons/`.
5.  Install the tray app template under `/usr/local/etc/network-router/`.
6.  Start the daemon service immediately.

**Note:** The tray icon is opt-in. Turn it on with `network-router tray-enable`.

### 2. Verify Operation

After installation, check the service status:

```bash
network-router status
```

If the installation is successful, you will see a **"Running"** status and information about current network connections.

## Configuration

The configuration file is located at:  
**`/usr/local/etc/network-router/config.yaml`**

You can edit this file to add company domains or IP ranges:

```yaml
# Service / hardware-port keywords, matched case-insensitively.
# These are NOT device numbers: do not put "en0" or "en8" here.
# Leave them commented out for automatic discovery.
# wifi_interface_name: "Wi-Fi"
# phone_interface_name: "iPhone USB"

# DNS Proxy configuration to support Wildcard Domains
dns_proxy_enabled: true
dns_proxy_port: 5454

# The proxy resolves tethered domains over the phone path, so name resolution and
# traffic use the same network. Upstreams are preferred in this order:
#   1. the DNS server the phone advertises over DHCP
#   2. dns_upstream_pinned below, each given a /32 route over the phone
#   3. the system resolver, as a degraded fallback that installs no routes
# Note: pinning resolvers to the phone sends DNS queries over cellular data.
dns_upstream_pinned:
  - 1.1.1.1:53
  - 8.8.8.8:53

# Leave empty to use the order above. Setting a value overrides it entirely.
dns_upstream: ''

# Answer AAAA/HTTPS/SVCB for tethered domains with an empty NOERROR so clients use
# the A records the /32 routes cover. Those query types would otherwise let a
# client connect over an address family that is not routed.
force_ipv4: true

# Automatically refresh routing on a schedule (Cron syntax)
route_refresh_cron: '0 * * * *'
auto_refresh_route: false

# List of domains to route through Phone (Tethering)
tether_domains:
  - 'github.com'
  - '*.githubcopilot.com'
  - 'perplexity.ai'
  - '*.perplexity.ai'
  - 'pplx.ai'
  - '*.pplx.ai'

# List of IP ranges to route through Phone (Tethering).
# Bare IP addresses belong here, not in tether_domains.
tether_cidrs:
  - '91.108.4.0/22'
  - '34.2.135.176/32'
```

**Note:** After editing the configuration file, you need to restart the service to apply changes:

```bash
network-router restart
```

## Usage Instructions

### System Tray (Opt-in)

After installation, the **tray icon is disabled by default** to save resources. You can enable it using the CLI:

```bash
# Enable and start tray icon (persists after reboot)
network-router tray-enable

# Disable and stop tray icon
network-router tray-disable
```

**Icon Status:**
*   🟢 **Green Icon**: Daemon is active and routes are applied.
*   ⚫ **Gray Icon**: Daemon is running but routes are not applied or auto-routing is disabled.
*   🔴 **Red Icon**: Cannot connect to the daemon.

**Menu controls:**
*   **Status Display**: Shows current status (read-only).
*   **Enable/Disable Auto-Routing**: Toggle automatic routing.
*   **Apply Routes**: Force apply routes now.
*   **Refresh Routes**: Update domain IPs and re-apply routes.
*   **Clear Routes**: Remove all routes.
*   **Enable/Disable DNS Proxy**: Toggle internal DNS Proxy.
*   **Enable/Disable Auto Refresh**: Toggle scheduled route updates.
*   **Show Debug**: Opens Terminal and runs `tail -f` to watch logs in real-time.
*   **Hide Icon**: Makes the icon invisible. Its menu bar slot stays in place and remains clickable, which is how you get the menu back to show it again.
*   **Quit**: Stops the tray app (can be restarted with `network-router tray`).

Menu actions post a native macOS notification on success or failure, and mirror the same text to `/tmp/network-router-tray.log`.

**Managing Tray App:**

```bash
# Check status
launchctl list | grep network-router.tray

# Enable/Start tray
network-router tray-enable

# Disable/Stop tray
network-router tray-disable

# Or run manually in foreground
network-router tray
```

### CLI Commands

If you prefer not to use the tray icon, you can still control it via terminal. Note: CLI commands **no longer require sudo** (unless socket permissions are incorrect).

#### View Status
Check if the daemon is running and which networks are active.
```bash
network-router status
```

#### Toggle Features (Enable/Disable)
Pause automatic routing (keeps service running but stops network interference).
```bash
network-router disable
network-router enable
```

#### Tray Management
```bash
network-router tray-enable   # Register and start tray icon
network-router tray-disable  # Stop and unregister tray icon
```

#### Manual Application (Apply/Clear)
Force the routing logic immediately (useful for testing new configs without waiting for auto-detect).
```bash
network-router apply    # Add routes
network-router clear    # Remove routes, return to default
network-router restart  # Clear then apply
network-router refresh  # Re-resolve domains and re-apply
```

Note that `clear` also turns auto-routing off, so the next network event does not
immediately re-apply. Use `enable` to turn it back on.

#### DNS Proxy and Scheduled Refresh
```bash
network-router enable-dns             # Start the DNS proxy
network-router disable-dns            # Stop the DNS proxy
network-router enable-auto-refresh    # Turn on the cron-driven refresh
network-router disable-auto-refresh   # Turn it off
```

The auto-refresh setting is held in memory only. Set `auto_refresh_route` in the
config file to change the startup default.

## Common Scenarios

### Scenario 1: Working from Home
1. Start tray app: `network-router tray`
2. Connect iPhone USB for high-speed Internet
3. Connect corporate WiFi for internal network access
4. ✅ Routes are applied automatically, traffic is separated

### Scenario 2: Coffee Shop (WiFi only, no tethering)
1. Only connect to shop WiFi
2. Routes are not applied, because no tethering interface is ready
3. Traffic goes through the default gateway as normal

### Scenario 3: Office (WiFi only)
1. Unplug iPhone USB
2. Routes are automatically cleared
3. All traffic goes through WiFi

## Development

### Build & Test

```bash
# Build project
make build

# Run daemon in foreground (for debugging)
make daemon

# Run tray app locally
make tray

# Vet and run the test suite with the race detector
make test

# Clean build artifacts
make clear-build
```

### Testing Tray App

1. Ensure daemon is running:
   ```bash
   sudo make daemon
   ```

2. Run tray app in another terminal:
   ```bash
   make tray
   ```

3. Verification:
   - Icon appears in menu bar
   - Click icon to see menu
   - Try Enable/Disable, Apply, Clear functions
   - Verify icon color changes according to status

### Technical Details

**Tray Implementation:**
- Library: `github.com/getlantern/systray`
- Polling interval: 5 seconds
- Icon generation: Dynamic PNG (32x32 pixels)
- IPC Protocol: JSON over Unix socket

**Socket Configuration:**
- Path: `/tmp/network-router.sock`
- Permissions: `0666` (allows user-mode tray app to connect to root daemon)
- Timeout: 5 seconds

**Security Notes:**
- Socket `0666` allows any local user to connect
- Suitable for single-user macOS system
- For higher security: use `0660` + group ownership or add authentication

## Tips & Best Practices

1. **Performance**: Routes are cached, only reapplied on network change.
2. **Battery & Memory**: 
   - **Daemon**: Minimal impact (~10-15MB RAM).
   - **Tray**: ~40MB RAM (Real Memory). 
   - *Note*: On macOS/Go, you might see a "Virtual Memory" of 400GB+ or "Memory" of 500MB+ in Activity Monitor. This is normal Go runtime behavior and does not represent actual RAM usage.
3. **Updates**: Pull new code, rebuild, and restart service.
4. **Config Backup**: Save a backup of `/usr/local/etc/network-router/config.yaml`
5. **Multiple Macs**: Same config works if using the same network names

## Troubleshooting

### Daemon Not Running

```bash
# Check launchd status
sudo launchctl list | grep network-router

# If not present, load manually
sudo launchctl load /Library/LaunchDaemons/com.bez.network-router.plist
```

### Tray App Cannot Connect

```bash
# Verify socket existence and permissions
ls -la /tmp/network-router.sock
# Should see: srw-rw-rw- (0666 permissions)

# If daemon is running but socket is missing, restart daemon
sudo launchctl unload /Library/LaunchDaemons/com.bez.network-router.plist
sudo launchctl load /Library/LaunchDaemons/com.bez.network-router.plist
```

### Routes Not Applied

```bash
# Check if interface names are correct
networksetup -listallhardwareports

# View current routing table
netstat -rn

# Try applying manually to see error messages
network-router apply

# Check daemon logs
tail -f /var/log/network-router.log

# Or use "Show Debug" in Tray Menu to open terminal quickly.
```

### Tethered Domains Fail to Resolve

Each domain in `tether_domains` has the proxy as its only resolver, so a proxy
problem appears as a connection failure rather than a slow lookup.

```bash
# Is the proxy listening?
sudo lsof -nP -iUDP:5454 -iTCP:5454

# Ask the proxy directly
dig @127.0.0.1 -p 5454 <configured-domain> A

# Compare the two paths, substituting your own device names
dig @"$(ipconfig getoption en0 domain_name_server)" <configured-domain> A
dig @"$(ipconfig getoption en7 domain_name_server)" <configured-domain> A

# Any upstream problems, or answers served from the degraded fallback?
grep -E "Resolution failed|system resolver" /var/log/network-router.log | tail
```

The daemon removes its own generated resolver files at startup. To clear them by
hand:

```bash
sudo grep -l "# Generated by Network Router" /etc/resolver/* | xargs sudo rm
sudo killall -HUP mDNSResponder
```

### Viewing Logs

```bash
# Live daemon logs
tail -f /var/log/network-router.log

# System logs
sudo grep "network-router" /var/log/system.log

# If log file doesn't exist, create it
sudo touch /var/log/network-router.log
sudo chmod 644 /var/log/network-router.log
```

## Uninstall

To completely remove the service from your machine:

```bash
# Run uninstall script
sudo ./uninstall_service.sh
```

Or remove manually:

```bash
# Stop daemon and tray app
sudo launchctl unload /Library/LaunchDaemons/com.bez.network-router.plist
launchctl unload ~/Library/LaunchAgents/com.bez.network-router.tray.plist
killall network-router 2>/dev/null || true

# Remove files
sudo rm /Library/LaunchDaemons/com.bez.network-router.plist
rm ~/Library/LaunchAgents/com.bez.network-router.tray.plist
sudo rm /usr/local/bin/network-router
sudo rm -rf /usr/local/etc/network-router
sudo rm -rf /usr/local/var/network-router
sudo rm /tmp/network-router.sock 2>/dev/null || true

# Remove generated resolver files
sudo grep -l "# Generated by Network Router" /etc/resolver/* 2>/dev/null | xargs sudo rm

echo "✅ Uninstall completed."
```

## Additional Documentation

- **[ARCHITECTURE.md](ARCHITECTURE.md)**: Details on system architecture and data flow
- **Issues**: https://github.com/tunglt1810/network-router/issues

## License

Apache License 2.0 — See `LICENSE` for details.
