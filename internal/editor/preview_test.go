package editor

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/config"
)

// Renders the image window for visual review; set SCREENPIN_UI_SCREENSHOTS.
func TestImageWindowPreview(t *testing.T) {
	out := os.Getenv("SCREENPIN_UI_SCREENSHOTS")
	if out == "" {
		t.Skip("set SCREENPIN_UI_SCREENSHOTS to a directory")
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 1200, 760))
	for y := 0; y < 760; y++ {
		for x := 0; x < 1200; x++ {
			pixels.SetRGBA(x, y, color.RGBA{uint8(235 - y/12), uint8(238 - x/40), 245, 255})
		}
	}
	for _, width := range []int{1000, 520} {
		for _, dark := range []bool{false, true} {
			picture, err := PictureFromPixels(pixels, "preview")
			if err != nil {
				t.Fatal(err)
			}
			a := New(config.Default())
			a.loadPicture(picture, nil)
			picture.Close()
			tt := ui.NewTester(a.view, width, 640)
			tt.SetDark(dark)
			f, err := os.Create(filepath.Join(out, fmt.Sprintf("editor-%d-dark-%t.png", width, dark)))
			if err != nil {
				t.Fatal(err)
			}
			_ = png.Encode(f, tt.Image())
			f.Close()
			a.Close()
		}
	}
}
