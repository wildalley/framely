package capture

import (
	"image"
	"image/color"
	"testing"
)

func TestCropPixelsDetachesNegativeOriginAndPaddedRows(t *testing.T) {
	desktop := image.NewRGBA(image.Rect(-200, -100, 200, 200))
	desktop.SetRGBA(-10, 20, color.RGBA{R: 123, G: 45, B: 67, A: 255})
	cropped, err := CropPixels(desktop.SubImage(image.Rect(-100, 0, 100, 100)).(*image.RGBA), image.Rect(-10, 20, 10, 40))
	if err != nil {
		t.Fatal(err)
	}
	if cropped.Bounds() != image.Rect(0, 0, 20, 20) || cropped.Stride != 80 || cropped.RGBAAt(0, 0) != (color.RGBA{R: 123, G: 45, B: 67, A: 255}) {
		t.Fatal("wrong crop layout or pixels")
	}
	desktop.SetRGBA(-10, 20, color.RGBA{A: 255})
	if cropped.RGBAAt(0, 0).R != 123 {
		t.Fatal("selection retained the desktop buffer")
	}
	if _, err = CropPixels(desktop, image.Rect(500, 500, 600, 600)); err == nil {
		t.Fatal("accepted out-of-bounds region")
	}
}
