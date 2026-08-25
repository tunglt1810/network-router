//go:build !darwin

package tray

import "time"

// ShowNotificationPopup is a no-op on non-macOS platforms.
func ShowNotificationPopup(title, message string, d time.Duration) {}
