package assets

import (
	_ "embed"
)

//go:embed icon_active.svg
var iconActiveSVG []byte

//go:embed icon_inactive.svg
var iconInactiveSVG []byte

//go:embed icon_error.svg
var iconErrorSVG []byte

// IconActive returns a green icon for active state
func IconActive() []byte {
	return iconActiveSVG
}

// IconInactive returns a gray icon for inactive state
func IconInactive() []byte {
	return iconInactiveSVG
}

// IconError returns a red icon for error state
func IconError() []byte {
	return iconErrorSVG
}
