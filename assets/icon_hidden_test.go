package assets

import (
	"bytes"
	"image/png"
	"testing"
)

func TestIconHiddenIsOneTransparentPixel(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(IconHidden()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 1 || b.Dy() != 1 {
		t.Fatalf("size %v, want 1x1", b)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Fatalf("alpha %d, want 0", a)
	}
}
