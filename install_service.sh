#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
print_step() {
    echo -e "${BLUE}[$(date +%H:%M:%S)]${NC} $1"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

# Check for root
if [ "$EUID" -ne 0 ]; then 
  print_error "Please run as root: sudo ./install_service.sh"
  exit 1
fi

echo ""
echo -e "${GREEN}╔═══════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║  Network Router Service Installer        ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════╝${NC}"
echo ""

# Define paths
BINARY_NAME="network-router"
BUILD_PATH="./build/$BINARY_NAME"
INSTALL_BIN_PATH="/usr/local/bin/$BINARY_NAME"
CONFIG_SRC="./config.yaml"
CONFIG_DIR="/usr/local/etc/network-router"
CONFIG_DEST="$CONFIG_DIR/config.yaml"
DAEMON_PLIST_SRC="./com.bez.network-router.plist"
DAEMON_PLIST_DEST="/Library/LaunchDaemons/com.bez.network-router.plist"
TRAY_PLIST_SRC="./com.bez.network-router.tray.plist"
TRAY_PLIST_DEST="$HOME/Library/LaunchAgents/com.bez.network-router.tray.plist"

# Get the real user (not root)
if [ -n "$SUDO_USER" ] && [ "$SUDO_USER" != "root" ]; then
    REAL_USER="$SUDO_USER"
elif [ -n "$USER" ] && [ "$USER" != "root" ]; then
    REAL_USER="$USER"
else
    # Try to detect the console user
    REAL_USER=$(stat -f "%Su" /dev/console)
fi
REAL_HOME=$(eval echo ~$REAL_USER)

# Validate we found a real user
if [ "$REAL_USER" = "root" ] || [ -z "$REAL_USER" ]; then
    print_error "Cannot determine real user. Please run with: sudo -u <user> ./install_service.sh"
    exit 1
fi

print_step "Installing for user: $REAL_USER (home: $REAL_HOME)"

# 1. Build (with auto-build)
print_step "Checking binary..."
if [ ! -f "$BUILD_PATH" ]; then
    print_warning "Binary not found. Building from source..."
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed. Please install Go first."
        exit 1
    fi
    go build -o "$BUILD_PATH" main.go
    print_success "Build completed"
else
    print_success "Binary found at $BUILD_PATH"
fi

# 2. Install Binary
print_step "Installing binary..."
cp "$BUILD_PATH" "$INSTALL_BIN_PATH"
chmod +x "$INSTALL_BIN_PATH"
print_success "Binary installed to $INSTALL_BIN_PATH"

# 3. Install Config
print_step "Installing configuration..."
mkdir -p "$CONFIG_DIR"
if [ -f "$CONFIG_DEST" ]; then
    print_warning "Existing config found, creating backup..."
    cp "$CONFIG_DEST" "${CONFIG_DEST}.backup.$(date +%Y%m%d_%H%M%S)"
    print_success "Backup created"
fi
cp "$CONFIG_SRC" "$CONFIG_DEST"
print_success "Config installed to $CONFIG_DEST"

# 4. Install Service Definition
print_step "Installing daemon service (root)..."
cp "$DAEMON_PLIST_SRC" "$DAEMON_PLIST_DEST"
chmod 644 "$DAEMON_PLIST_DEST"
chown root:wheel "$DAEMON_PLIST_DEST"
print_success "Daemon plist installed"

# 5. Install Tray LaunchAgent
print_step "Installing tray agent (user: $REAL_USER)..."
TRAY_PLIST_USER_DEST="$REAL_HOME/Library/LaunchAgents/com.bez.network-router.tray.plist"
mkdir -p "$REAL_HOME/Library/LaunchAgents"
cp "$TRAY_PLIST_SRC" "$TRAY_PLIST_USER_DEST"
chmod 644 "$TRAY_PLIST_USER_DEST"
chown "$REAL_USER:staff" "$TRAY_PLIST_USER_DEST"
print_success "Tray agent plist installed"

# 6. Load Daemon Service
print_step "Starting daemon service..."

# Check if already loaded
if launchctl list | grep -q "com.bez.network-router"; then
    print_warning "Service already loaded, unloading first..."
    launchctl bootout system/com.bez.network-router 2>/dev/null || true
    launchctl unload "$DAEMON_PLIST_DEST" 2>/dev/null || true
    sleep 1
    # Force kill any existing process
    pkill -9 -f "network-router daemon" 2>/dev/null || true
    sleep 1
fi

# Try bootstrap first, fallback to load
if launchctl bootstrap system "$DAEMON_PLIST_DEST" 2>/dev/null; then
    print_success "Daemon bootstrapped"
elif launchctl load "$DAEMON_PLIST_DEST" 2>/dev/null; then
    print_success "Daemon loaded"
else
    print_error "Failed to start daemon service"
    print_warning "Check logs: /var/log/network-router.log"
    exit 1
fi

sleep 2

# Verify daemon is running
if launchctl list | grep -q "com.bez.network-router"; then
    print_success "Daemon service started"
else
    print_error "Failed to start daemon service"
    print_warning "Check logs: /var/log/network-router.log"
    exit 1
fi

# 7. Load Tray Agent (as user)
print_step "Starting tray agent..."
REAL_UID=$(id -u "$REAL_USER")

# Unload if exists
launchctl asuser "$REAL_UID" sudo -u "$REAL_USER" launchctl unload "$TRAY_PLIST_USER_DEST" 2>/dev/null || true

# Bootstrap the agent
if launchctl bootstrap "gui/$REAL_UID" "$TRAY_PLIST_USER_DEST" 2>/dev/null; then
    print_success "Tray agent registered"
    # Kickstart it to run immediately
    if launchctl kickstart -k "gui/$REAL_UID/com.bez.network-router.tray" 2>/dev/null; then
        print_success "Tray agent started"
        TRAY_STARTED=true
    fi
else
    # Fallback: try load with asuser
    if launchctl asuser "$REAL_UID" sudo -u "$REAL_USER" launchctl load "$TRAY_PLIST_USER_DEST" 2>/dev/null; then
        print_success "Tray agent loaded"
        TRAY_STARTED=true
    else
        print_warning "Could not auto-start tray agent via launchctl"
        # Last attempt: run binary directly in background as user
        print_step "Attempting direct launch..."
        if sudo -u "$REAL_USER" nohup "$INSTALL_BIN_PATH" tray >/tmp/network-router-tray-launch.log 2>&1 &
        then
            sleep 3
            if pgrep -f "network-router tray" > /dev/null; then
                print_success "Tray app launched directly"
                TRAY_STARTED=true
            fi
        fi
    fi
fi

sleep 2

# Verify tray is running
if [ "$TRAY_STARTED" != true ]; then
    if launchctl print "gui/$REAL_UID/com.bez.network-router.tray" &>/dev/null 2>&1; then
        print_success "Tray agent registered (will start on next login)"
        echo ""
        print_warning "Tray icon not visible yet. Start it manually:"
        echo "  $INSTALL_BIN_PATH tray &"
    else
        print_warning "Please start tray manually: $INSTALL_BIN_PATH tray"
    fi
fi

# 8. Health check
print_step "Running health check..."
sleep 1
if [ -S "/tmp/network-router.sock" ]; then
    print_success "IPC socket created"
    # Test connection using launchctl asuser for proper context
    if launchctl asuser "$REAL_UID" sudo -u "$REAL_USER" "$INSTALL_BIN_PATH" status &>/dev/null; then
        print_success "Daemon responding to commands"
    else
        print_warning "Daemon not responding yet (may need more time)"
    fi
else
    print_warning "IPC socket not found (daemon may still be starting)"
fi

echo ""
echo -e "${GREEN}╔═══════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║  Installation Complete!                   ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════╝${NC}"
echo ""
echo -e "${BLUE}Services:${NC}"
echo "  • Daemon: com.bez.network-router (running)"
if [ "$TRAY_STARTED" = true ]; then
    echo "  • Tray:   com.bez.network-router.tray (running)"
else
    echo "  • Tray:   com.bez.network-router.tray (installed, auto-start on login)"
fi
echo ""
echo -e "${BLUE}Commands:${NC}"
echo "  • Status:  network-router status"
echo "  • Enable:  network-router enable"
echo "  • Disable: network-router disable"
echo "  • Clear:   network-router clear"
if [ "$TRAY_STARTED" != true ]; then
    echo -e "  • ${YELLOW}Start Tray: network-router tray${NC}"
fi
echo ""
echo -e "${BLUE}Logs:${NC}"
echo "  • Daemon: /var/log/network-router.log"
echo "  • Tray:   /tmp/network-router-tray.log"
echo ""
if [ "$TRAY_STARTED" = true ]; then
    echo -e "${GREEN}✓ Tray icon should be visible in menu bar!${NC}"
else
    echo -e "${YELLOW}⚡ To see tray icon now, run: network-router tray${NC}"
    echo -e "${BLUE}   (Or it will auto-start on next login)${NC}"
fi
echo ""
