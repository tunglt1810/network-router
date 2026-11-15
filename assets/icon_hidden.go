package assets

import (
	"bytes"
	"image"
	"image/png"
)

var iconHidden = func() []byte {
	var buf bytes.Buffer
	// A new RGBA image is fully transparent.
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	return buf.Bytes()
}()

// IconHidden returns a 1x1 transparent PNG for hiding the icon
func IconHidden() []byte {
	return iconHidden
}
