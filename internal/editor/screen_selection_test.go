package editor

import (
	"image"
	"runtime"
	"testing"

	"github.com/egoist/mygo"
	"github.com/wildalley/framely/internal/config"
)

func TestFreshScreenshotSelectionCanStartScrollWithoutReselecting(t *testing.T) {
	left, right := mygo.Rectangle{X: -1280, Width: 1280, Height: 720}, mygo.Rectangle{Width: 1920, Height: 1080}
	displays := []mygo.Display{{ID: 0, Bounds: right}, {ID: 1, Bounds: left}}
	for _, tc := range []struct {
		target mygo.Rectangle
		region image.Rectangle
		want   image.Rectangle
	}{
		{left, image.Rect(100, 100, 600, 650), image.Rect(200, 200, 1200, 1300)},
		{right, image.Rect(1400, 100, 3000, 1000), image.Rect(240, 200, 3440, 2000)},
	} {
		bounds := image.Rect(0, 0, 3200, 1080)
		region := tc.region
		if runtime.GOOS == "windows" {
			bounds = bounds.Sub(image.Pt(1280, 0))
			region = region.Sub(image.Pt(1280, 0))
		}
		selection, err := ScreenSelectionFromDesktop(region, &tc.target, displays, bounds)
		if err != nil {
			t.Fatal(err)
		}
		reused, err := selection.ForStream(selection.Size.Mul(2))
		if err != nil || reused != tc.want || selection.Display != tc.target {
			t.Fatal("screenshot selection moved when reused", reused, err)
		}
		picture, err := PictureFromPixels(image.NewRGBA(image.Rect(0, 0, 500, 550)), "截图")
		defer picture.Close()
		if err != nil {
			t.Fatal(err)
		}
		picture = picture.WithScreenSelection(selection)
		selection.Region = image.Rectangle{} // picture owns its metadata
		a := New(config.Default())
		a.loadPicture(picture, &tc.target)
		copy := a.ScreenSelection()
		if copy == nil || copy.Region.Empty() {
			t.Fatal("image lost its live desktop selection")
		}
		copy.Region = image.Rectangle{}
		if a.ScreenSelection().Region.Empty() {
			t.Fatal("caller changed the image's original selection")
		}
		a.release()
		if a.selection != nil || a.ScreenSelection() != nil {
			t.Fatal("closed image retained a reusable desktop region")
		}
	}
}

func TestImportedImageAndInvalidSelectionDoNotReuseDesktopCoordinates(t *testing.T) {
	picture, err := PictureFromPixels(image.NewRGBA(image.Rect(0, 0, 500, 550)), "打开的图片")
	defer picture.Close()
	if err != nil {
		t.Fatal(err)
	}
	a := New(config.Default())
	defer a.release()
	a.loadPicture(picture, nil)
	if a.ScreenSelection() != nil {
		t.Fatal("imported or stitched image acquired a desktop region")
	}
	for _, s := range []ScreenSelection{{}, {Region: image.Rect(-1, 0, 100, 200), Size: image.Pt(800, 600)}, {Region: image.Rect(700, 400, 900, 800), Size: image.Pt(800, 600)}} {
		if _, err := s.ForStream(image.Pt(1600, 1200)); err == nil {
			t.Fatal("invalid screenshot region could be reused")
		}
	}
}
