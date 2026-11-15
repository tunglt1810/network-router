//go:build darwin

package tray

import (
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"

	"network-router/assets"
	"network-router/client"
	"network-router/daemon"

	"github.com/getlantern/systray"
)

// hideIconTooltip describes what hiding actually does: the icon is replaced by a
// transparent pixel, so its menu bar slot remains clickable.
const hideIconTooltip = "Make the icon invisible; its menu bar slot stays clickable"

// TrayApp manages the system tray application
type TrayApp struct {
	client *client.Client

	// mu guards lastStatus and iconHidden. The status poller, the delayed
	// refreshes, and the menu handler all run on different goroutines.
	mu         sync.Mutex
	lastStatus *daemon.RouterStatus
	iconHidden bool

	// Menu items
	mStatus   *systray.MenuItem
	mToggle   *systray.MenuItem
	mRefresh  *systray.MenuItem
	mDebug    *systray.MenuItem
	mHideIcon *systray.MenuItem
	mQuit     *systray.MenuItem
}

// NewTrayApp creates a new tray application
func NewTrayApp() *TrayApp {
	return &TrayApp{
		client: client.NewClient(),
	}
}

// Run starts the tray application
func (t *TrayApp) Run() {
	systray.Run(t.onReady, t.onExit)
}

// onReady is called when the tray is ready
func (t *TrayApp) onReady() {
	systray.SetTooltip("Network Router Daemon")

	// Set initial icon
	t.updateIcon(false, false)

	// Create menu items
	t.mStatus = systray.AddMenuItem("📊 Network Router - Status: Checking...", "Current daemon status")
	t.mStatus.Disable()

	systray.AddSeparator()

	t.mToggle = systray.AddMenuItem("🤖 Turn Routing On", "Turn routing on or off")
	t.mRefresh = systray.AddMenuItem("🔄 Refresh Routes", "Re-resolve IPs and re-apply")

	systray.AddSeparator()
	t.mDebug = systray.AddMenuItem("🔍 Show Debug", "Follow service logs in terminal")

	systray.AddSeparator()

	t.mHideIcon = systray.AddMenuItem("🕶️ Hide Icon", hideIconTooltip)

	systray.AddSeparator()

	t.mQuit = systray.AddMenuItem("🚪 Quit", "Exit the application")

	// Start status polling
	go t.pollStatus()

	// Handle menu clicks
	go t.handleMenuClicks()
}

// onExit is called when the tray is exiting
func (t *TrayApp) onExit() {
	log.Println("Tray application exiting")
}

// pollStatus polls the daemon status periodically
func (t *TrayApp) pollStatus() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Initial status check
	t.updateStatus()

	for range ticker.C {
		t.updateStatus()
	}
}

// updateStatus fetches and updates the status
func (t *TrayApp) updateStatus() {
	resp, err := t.client.SendRequest(daemon.ActionStatus)
	if err != nil {
		t.setStatus(nil)
		t.updateIcon(false, true)
		t.mStatus.SetTitle("📊 Network Router - Status: Disconnected")
		t.mStatus.SetTooltip(fmt.Sprintf("Error: %v", err))
		t.mToggle.Disable()
		t.mRefresh.Disable()
		return
	}

	if !resp.Success || resp.Data == nil {
		t.setStatus(nil)
		t.updateIcon(false, true)
		t.mStatus.SetTitle("📊 Network Router - Status: Error")
		return
	}

	data := resp.Data
	t.setStatus(data)
	t.updateIcon(data.AutoRoutingEnabled && data.RoutesApplied, false)

	statusText := fmt.Sprintf("📊 Network Router - On: %v | Routes: %v",
		formatBool(data.AutoRoutingEnabled), formatBool(data.RoutesApplied))
	tooltip := fmt.Sprintf("Phone: %v", formatBool(data.PhoneReady))
	if data.PhoneStatusReason != "" {
		tooltip = fmt.Sprintf("%s | %s", tooltip, data.PhoneStatusReason)
	}

	t.mStatus.SetTitle(statusText)
	t.mStatus.SetTooltip(tooltip)

	if data.AutoRoutingEnabled {
		t.mToggle.SetTitle("🤖 Turn Routing Off")
	} else {
		t.mToggle.SetTitle("🤖 Turn Routing On")
	}

	t.mToggle.Enable()
	t.mRefresh.Enable()
}

func (t *TrayApp) setStatus(status *daemon.RouterStatus) {
	t.mu.Lock()
	t.lastStatus = status
	t.mu.Unlock()
}

// status returns the last successful status, or nil.
func (t *TrayApp) status() *daemon.RouterStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastStatus
}

// updateIcon updates the tray icon based on state
func (t *TrayApp) updateIcon(active bool, failed bool) {
	t.mu.Lock()
	hidden := t.iconHidden
	t.mu.Unlock()
	if hidden {
		return
	}

	switch {
	case failed:
		systray.SetIcon(assets.IconError())
	case active:
		systray.SetIcon(assets.IconActive())
	default:
		systray.SetIcon(assets.IconInactive())
	}
}

// handleMenuClicks handles menu item clicks
func (t *TrayApp) handleMenuClicks() {
	for {
		select {
		case <-t.mToggle.ClickedCh:
			t.toggleRouting()
		case <-t.mRefresh.ClickedCh:
			t.run(daemon.ActionRefresh, "Failed to refresh routes")
		case <-t.mHideIcon.ClickedCh:
			t.handleHideIcon()
		case <-t.mDebug.ClickedCh:
			t.handleDebug()
		case <-t.mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

// run sends an action and shows the daemon's message. A failure includes a
// response with success=false.
func (t *TrayApp) run(action, failure string) {
	resp, err := t.client.SendRequest(action)
	switch {
	case err != nil:
		t.showNotification("Error", fmt.Sprintf("%s: %v", failure, err))
	case !resp.Success:
		t.showNotification("Error", resp.Message)
	default:
		t.showNotification("Success", resp.Message)
	}
	time.AfterFunc(500*time.Millisecond, t.updateStatus)
}

// toggleRouting turns routing off when the last status reports it on, and on
// otherwise. It does nothing before the first successful status.
func (t *TrayApp) toggleRouting() {
	status := t.status()
	if status == nil {
		return
	}
	action := daemon.ActionOn
	if status.AutoRoutingEnabled {
		action = daemon.ActionOff
	}
	t.run(action, "Failed to switch routing")
}

// handleHideIcon toggles icon visibility
func (t *TrayApp) handleHideIcon() {
	t.mu.Lock()
	t.iconHidden = !t.iconHidden
	hidden := t.iconHidden
	t.mu.Unlock()

	if hidden {
		// The icon becomes a transparent 1x1 pixel. The menu bar slot is still
		// there and still clickable, which is how the menu stays reachable.
		systray.SetIcon(assets.IconHidden())
		t.mHideIcon.SetTitle("🕶️ Show Icon")
		t.mHideIcon.SetTooltip("Show the tray icon again")
		log.Println("Icon hidden - its menu bar slot is still clickable")
		return
	}

	status := t.status()
	t.updateIcon(status != nil && status.AutoRoutingEnabled && status.RoutesApplied, false)
	t.mHideIcon.SetTitle("🕶️ Hide Icon")
	t.mHideIcon.SetTooltip(hideIconTooltip)
	log.Println("Icon shown")
}

// handleDebug opens a terminal to tail the logs
func (t *TrayApp) handleDebug() {
	logPath := "/var/log/network-router.log"
	script := fmt.Sprintf(`
		tell application "Terminal"
			activate
			do script "tail -f %s"
		end tell`, logPath)

	cmd := exec.Command("osascript", "-e", script)
	err := cmd.Run()
	if err != nil {
		log.Printf("Debug error: %v", err)
		t.showNotification("Error", fmt.Sprintf("Failed to open terminal: %v", err))
	}
}

// showNotification displays a transient single-item popup from the tray icon
// and mirrors the message to the log.
func (t *TrayApp) showNotification(title, message string) {
	log.Printf("[%s] %s", title, message)
	ShowNotificationPopup(title, message, 3*time.Second)
}

// formatBool formats a boolean as ✓/✗
func formatBool(b bool) string {
	if b {
		return "✓"
	}
	return "✗"
}
