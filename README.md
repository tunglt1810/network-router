# Network Router Daemon

A service that automatically manages network routing on macOS, optimizing connections when using Wifi (for internal/corporate network) and USB Tethering/Ethernet (for high-speed Internet) simultaneously.

This project has evolved from a simple CLI tool into a **System Daemon + CLI Client + System Tray** architecture, allowing it to run in the background automatically and stably without manual intervention on every system boot.

## ⚡ Quick Start - Install with one command

```bash
git clone https://github.com/bez/network-router.git && cd network-router && sudo make install
```

Once installed, the tray icon will automatically appear in your menu bar! 🎉

## Features

*   **Background Daemon**: Runs as a system service (root), automatically starts with macOS.
*   **System Tray Icon**: Graphical interface on the menu bar for easy control, no terminal required.
*   **Auto-Switching**: Automatically detects when you plug/unplug iPhone USB Tethering or connect to Wifi to adjust the Routing Table immediately.
*   **Split Tunneling**:
    *   **Internet**: Routes external traffic through the fastest connection (e.g., USB Tethering).
    *   **Internal**: Routes internal domains (`*.corp.com`) and private IPs (`192.168.x.x`) through Wifi.
*   **CLI Client**: Command-line interface to check status or toggle services easily.
*   **DNS Proxy**: Perfect support for wildcard domain routing by intercepting DNS requests.
*   **Auto Refresh**: Automatically updates domain IPs on a schedule (Cron).
*   **Auto-disable on Clear**: Automatically disables auto-routing when routes are cleared to prevent unintended re-application.

## Architecture

*   **Daemon (`network-router daemon`)**:
    *   Runs with **root** privileges.
    *   Listens for network changes (Network Monitor).
    *   Executes `route` and `networksetup` commands.
    *   Opens an IPC socket at `/tmp/network-router.sock` to receive control commands.
*   **Tray App (`network-router tray`)**:
    *   Runs with **user** privileges.
    *   Displays an icon in the menu bar.
    *   Communicates with the Daemon via Unix socket.
    *   Control Menu: Enable/Disable, Apply, Clear, Status.
*   **Client (`network-router <cmd>`)**:
    *   User-executed commands to communicate with the Daemon via socket.

## 📦 Installation

### Method 1: Automatic Installation (Recommended)

Just run a single command:

```bash
git clone https://github.com/bez/network-router.git && cd network-router && sudo make install
```

The script will automatically:
- ✅ Build binary from source code
- ✅ Install to `/usr/local/bin/network-router`
- ✅ Install config file to `/usr/local/etc/network-router/`
- ✅ Register and start daemon service (LaunchDaemon)
- ✅ Register and start tray app (LaunchAgent)
- ✅ Run health check to verify service
- ✅ Display tray icon in the menu bar

### Method 2: Manual Installation (Step-by-step)

If you want more detailed control:

```bash
# Clone repository
git clone https://github.com/bez/network-router.git
cd network-router

# Build binary
make build

# Install service (requires sudo password)
sudo ./install_service.sh
```

The installation script will:
1.  Build the `network-router` binary and copy it to `/usr/local/bin/`.
2.  Create a configuration directory at `/usr/local/etc/network-router/`.
3.  Install the daemon service into `/Library/LaunchDaemons/`.
4.  **Install the tray app to run automatically at login** in `~/Library/LaunchAgents/`.
5.  Start both the daemon and tray app immediately.

**Note:** The tray icon will automatically appear in the menu bar after installation! 🎉

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
# Internal domains that should go through Wifi
internal_domains:
  - "*.internal.company.com"
  - "gitlab.local"

# Internal IP ranges that should go through Wifi
internal_cidrs:
  - "192.168.1.0/24"
  - "10.0.0.0/8"

# Interface names (usually no need to change on English macOS)
wifi_interface_name: "en0"
phone_interface_name: "en8"

# DNS Proxy configuration to support Wildcard Domains
dns_proxy_enabled: true
dns_proxy_port: 5454

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

# List of IP ranges to route through Phone (Tethering)
tether_cidrs:
  - '91.108.4.0/22'
```

**Note:** After editing the configuration file, you need to restart the service to apply changes:

```bash
network-router restart
```

## Usage Instructions

### System Tray (Default)

After installation, the **tray icon automatically appears** in the menu bar every time you log in. No manual command needed!

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
*   **Show Debug**: Opens Terminal (or iTerm2) and runs `tail -f` to watch logs in real-time.
*   **Hide Icon**: Hides the icon from the menu bar (still runs in background).
*   **Quit**: Stops the tray app (can be restarted with `network-router tray`).

**Managing Tray App:**
```bash
# Check status
launchctl list | grep network-router.tray

# Disable tray app (temporary)
launchctl unload ~/Library/LaunchAgents/com.bez.network-router.tray.plist

# Re-enable
launchctl load ~/Library/LaunchAgents/com.bez.network-router.tray.plist

# Or run manually
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

#### Manual Application (Apply/Clear)
Force the routing logic immediately (useful for testing new configs without waiting for auto-detect).
```bash
network-router apply   # Add routes
network-router clear   # Remove routes, return to default
```

## Common Scenarios

### Scenario 1: Working from Home
1. Start tray app: `network-router tray`
2. Connect iPhone USB for high-speed Internet
3. Connect corporate WiFi for internal network access
4. ✅ Routes are applied automatically, traffic is separated

### Scenario 2: Coffee Shop (No corporate WiFi)
1. Only connect to shop WiFi
2. Routes are not applied (no matching WiFi name)
3. Traffic goes through default gateway as normal

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

# Test icon generation
make test-icons

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

1. **Performance**: Routes are cached, only reapplied on network change
2. **Battery**: Minimal impact (~5-10MB RAM, negligible CPU)
3. **Updates**: Pull new code, rebuild, and restart service
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
sudo rm /tmp/network-router.sock 2>/dev/null || true

echo "✅ Uninstall completed."
```

## Additional Documentation

- **[ARCHITECTURE.md](ARCHITECTURE.md)**: Details on system architecture and data flow
- **Issues**: https://github.com/bez/network-router/issues

## License

Apache License 2.0 — See `LICENSE` for details.
