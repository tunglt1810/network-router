package tray

import (
	"fmt"
	"log"
	"network-router/assets"
	"network-router/client"
	"time"

	"github.com/getlantern/systray"
)

// TrayApp manages the system tray application
type TrayApp struct {
	client     *client.Client
	lastStatus *client.IPCResponse
	iconHidden bool

	// Menu items
	mStatus    *systray.MenuItem
	mToggle    *systray.MenuItem
	mApply     *systray.MenuItem
	mRefresh   *systray.MenuItem
	mClear     *systray.MenuItem
	mHideIcon  *systray.MenuItem
	mSeparator *systray.MenuItem
	mQuit      *systray.MenuItem
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

	t.mToggle = systray.AddMenuItem("🤖 Enable Auto-Routing", "Toggle auto-routing")
	t.mApply = systray.AddMenuItem("⚡ Apply Routes", "Force apply routes now")
	t.mRefresh = systray.AddMenuItem("🔄 Refresh Routes", "Re-resolve IPs and re-apply")
	t.mClear = systray.AddMenuItem("🗑️ Clear Routes", "Remove all routes")

	systray.AddSeparator()

	t.mHideIcon = systray.AddMenuItem("🕶️ Hide Icon", "Hide tray icon (show with Cmd+Opt+Shift+R)")

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
	resp, err := t.client.SendRequest("status", nil)
	if err != nil {
		t.lastStatus = nil
		t.updateIcon(false, true)
		t.mStatus.SetTitle("📊 Network Router - Status: Disconnected")
		t.mStatus.SetTooltip(fmt.Sprintf("Error: %v", err))
		t.mToggle.Disable()
		t.mApply.Disable()
		t.mRefresh.Disable()
		t.mClear.Disable()
		return
	}

	t.lastStatus = resp

	if !resp.Success || resp.Data == nil {
		t.updateIcon(false, true)
		t.mStatus.SetTitle("📊 Network Router - Status: Error")
		return
	}

	// Extract status data
	autoRouting := false
	routesApplied := false
	wifiActive := false
	phoneActive := false

	if val, ok := resp.Data["auto_routing_enabled"].(bool); ok {
		autoRouting = val
	}
	if val, ok := resp.Data["routes_applied"].(bool); ok {
		routesApplied = val
	}
	if val, ok := resp.Data["wifi_active"].(bool); ok {
		wifiActive = val
	}
	if val, ok := resp.Data["phone_active"].(bool); ok {
		phoneActive = val
	}

	// Update icon based on state
	t.updateIcon(autoRouting && routesApplied, false)

	// Update status text with "Network Router" prefix
	statusText := fmt.Sprintf("📊 Network Router - Auto: %v | Routes: %v",
		formatBool(autoRouting), formatBool(routesApplied))
	tooltip := fmt.Sprintf("WiFi: %v | Phone: %v",
		formatBool(wifiActive), formatBool(phoneActive))

	t.mStatus.SetTitle(statusText)
	t.mStatus.SetTooltip(tooltip)

	// Update toggle button
	if autoRouting {
		t.mToggle.SetTitle("🤖 Disable Auto-Routing")
	} else {
		t.mToggle.SetTitle("🤖 Enable Auto-Routing")
	}

	// Enable all controls when connected
	t.mToggle.Enable()
	t.mApply.Enable()
	t.mRefresh.Enable()
	t.mClear.Enable()
}

// updateIcon updates the tray icon based on state
func (t *TrayApp) updateIcon(active bool, error bool) {
	// Don't update icon if it's hidden
	if t.iconHidden {
		return
	}

	var icon []byte
	if error {
		icon = assets.IconError()
	} else if active {
		icon = assets.IconActive()
	} else {
		icon = assets.IconInactive()
	}
	systray.SetIcon(icon)
}

// handleMenuClicks handles menu item clicks
func (t *TrayApp) handleMenuClicks() {
	for {
		select {
		case <-t.mToggle.ClickedCh:
			t.handleToggle()
		case <-t.mApply.ClickedCh:
			t.handleApply()
		case <-t.mRefresh.ClickedCh:
			t.handleRefresh()
		case <-t.mClear.ClickedCh:
			t.handleClear()
		case <-t.mHideIcon.ClickedCh:
			t.handleHideIcon()
		case <-t.mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

// handleToggle toggles auto-routing
func (t *TrayApp) handleToggle() {
	if t.lastStatus == nil || t.lastStatus.Data == nil {
		return
	}

	autoRouting := false
	if val, ok := t.lastStatus.Data["auto_routing_enabled"].(bool); ok {
		autoRouting = val
	}

	var err error
	if autoRouting {
		_, err = t.client.SendRequest("disable", nil)
	} else {
		_, err = t.client.SendRequest("enable", nil)
	}

	if err != nil {
		log.Printf("Toggle error: %v", err)
		t.showNotification("Error", fmt.Sprintf("Failed to toggle: %v", err))
	} else {
		// Immediately update status
		time.AfterFunc(500*time.Millisecond, t.updateStatus)
	}
}

// handleApply applies routes
func (t *TrayApp) handleApply() {
	_, err := t.client.SendRequest("apply", nil)
	if err != nil {
		log.Printf("Apply error: %v", err)
		t.showNotification("Error", fmt.Sprintf("Failed to apply routes: %v", err))
	} else {
		t.showNotification("Success", "Routes applied")
		time.AfterFunc(500*time.Millisecond, t.updateStatus)
	}
}

// handleRefresh refreshes (re-resolves) routes
func (t *TrayApp) handleRefresh() {
	_, err := t.client.SendRequest("refresh", nil)
	if err != nil {
		log.Printf("Refresh error: %v", err)
		t.showNotification("Error", fmt.Sprintf("Failed to refresh routes: %v", err))
	} else {
		t.showNotification("Success", "Routes filtering refreshed")
		time.AfterFunc(500*time.Millisecond, t.updateStatus)
	}
}

// handleClear clears routes
func (t *TrayApp) handleClear() {
	_, err := t.client.SendRequest("clear", nil)
	if err != nil {
		log.Printf("Clear error: %v", err)
		t.showNotification("Error", fmt.Sprintf("Failed to clear routes: %v", err))
	} else {
		t.showNotification("Success", "Routes cleared")
		time.AfterFunc(500*time.Millisecond, t.updateStatus)
	}
}

// handleHideIcon toggles icon visibility
func (t *TrayApp) handleHideIcon() {
	t.iconHidden = !t.iconHidden

	if t.iconHidden {
		// Hide icon by setting it to a transparent 1x1 pixel
		systray.SetIcon(assets.IconHidden())
		t.mHideIcon.SetTitle("🕶️ Show Icon")
		t.mHideIcon.SetTooltip("Show tray icon")
		log.Println("Icon hidden - use menu or hotkey to show")
	} else {
		// Restore icon based on current state
		if t.lastStatus != nil && t.lastStatus.Success && t.lastStatus.Data != nil {
			autoRouting := false
			routesApplied := false
			if val, ok := t.lastStatus.Data["auto_routing_enabled"].(bool); ok {
				autoRouting = val
			}
			if val, ok := t.lastStatus.Data["routes_applied"].(bool); ok {
				routesApplied = val
			}
			t.updateIcon(autoRouting && routesApplied, false)
		} else {
			t.updateIcon(false, false)
		}
		t.mHideIcon.SetTitle("🕶️ Hide Icon")
		t.mHideIcon.SetTooltip("Hide tray icon (show with Cmd+Opt+Shift+R)")
		log.Println("Icon shown")
	}
}

// showNotification shows a notification (placeholder - could use native notifications)
func (t *TrayApp) showNotification(title, message string) {
	log.Printf("[%s] %s", title, message)
	// On macOS, you could use osascript for notifications:
	// exec.Command("osascript", "-e", fmt.Sprintf(`display notification "%s" with title "%s"`, message, title)).Run()
}

// formatBool formats a boolean as ✓/✗
func formatBool(b bool) string {
	if b {
		return "✓"
	}
	return "✗"
}
