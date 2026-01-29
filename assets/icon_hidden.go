package assets

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// IconHidden returns a 1x1 transparent PNG for hiding the icon
func IconHidden() []byte {
	// Create a 1x1 transparent image
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 0, G: 0, B: 0, A: 0})

	// Encode to PNG
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}
