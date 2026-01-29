package assets

import (
	_ "embed"
)

//go:embed icon_active.png
var iconActive []byte

//go:embed icon_inactive.png
var iconInactive []byte

//go:embed icon_error.png
var iconError []byte

// IconActive returns a green icon for active state
func IconActive() []byte {
	return iconActive
}

// IconInactive returns a gray icon for inactive state
func IconInactive() []byte {
	return iconInactive
}

// IconError returns a red icon for error state
func IconError() []byte {
	return iconError
}
