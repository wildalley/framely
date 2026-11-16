package desktop

import (
	"image"
	"testing"

	"github.com/egoist/mygo"
)

func TestStreamDisplayResolutionPreservesMonitorIdentity(t *testing.T) {
	displays := []mygo.Display{
		{ID: 1, Bounds: mygo.Rectangle{X: -1920, Y: 0, Width: 1920, Height: 1080}},
		{ID: 2, Bounds: mygo.Rectangle{X: 0, Y: -720, Width: 1280, Height: 720}},
	}
	bounds := image.Rect(0, -720, 1280, 0)
	if got := matchStreamDisplay(bounds, 1, displays); got == nil || got.ID != 2 {
		t.Fatalf("monitor coordinates lost: %v", got)
	}
	if got := matchStreamDisplay(image.Rectangle{}, 1, displays); got != nil {
		t.Fatalf("ambiguous multi-monitor source selected a display: %v", got)
	}
	if got := matchStreamDisplay(image.Rectangle{}, 1, displays[:1]); got == nil || got.ID != 1 {
		t.Fatalf("single-monitor source was not resolved: %v", got)
	}
	for _, sourceBounds := range []image.Rectangle{{}, bounds} {
		if got := matchStreamDisplay(sourceBounds, 2, displays[1:]); got != nil {
			t.Fatalf("window source inherited a monitor crop: %v", got)
		}
	}
}
