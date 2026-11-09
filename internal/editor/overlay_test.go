package editor

import (
	"image"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/doc"
)

func TestPhysicalDisplayAndSelectionOnMixedDPIMonitors(t *testing.T) {
	for _, tc := range []struct {
		bounds mygo.Rectangle
		scale  float64
		want   image.Rectangle
	}{
		{mygo.Rectangle{X: -1280, Y: -720, Width: 1280, Height: 720}, 1.5, image.Rect(-1920, -1080, 0, 0)},
		{mygo.Rectangle{X: 0, Y: 0, Width: 1920, Height: 1080}, 1, image.Rect(0, 0, 1920, 1080)},
		{mygo.Rectangle{X: 960, Y: 0, Width: 1280, Height: 720}, 2, image.Rect(1920, 0, 4480, 1440)},
	} {
		d := mygo.Display{Bounds: tc.bounds, ScaleFactor: tc.scale}
		crop := physicalDisplay(d)
		if crop != tc.want {
			t.Fatalf("physical bounds: %v != %v", crop, tc.want)
		}
		selection, err := regionToCrop(doc.Rect{X: 100, Y: 100, W: 200, H: 200}, float64(d.Bounds.Width), float64(d.Bounds.Height), crop)
		if err != nil || selection.Min.X != crop.Min.X+int(100*tc.scale) || selection.Min.Y != crop.Min.Y+int(100*tc.scale) || selection.Dx() != int(200*tc.scale) {
			t.Fatalf("selection: %v %v", selection, err)
		}
	}
}

func TestDesktopScrollSelectionMapsDisplayGeometryAndStreamScale(t *testing.T) {
	left := mygo.Rectangle{X: -1280, Y: 0, Width: 1280, Height: 720}
	right := mygo.Rectangle{Width: 1920, Height: 1080}
	// Primary-first enumeration and identical labels must not select the
	// primary display when the user actually drags on the left one.
	displays := []mygo.Display{{ID: 0, Label: "same", Bounds: right, Primary: true}, {ID: 1, Label: "same", Bounds: left, ScaleFactor: 2}}
	for _, tc := range []struct {
		target      mygo.Rectangle
		region      image.Rectangle
		desktopSize image.Point
		sourceSize  image.Point
		want        image.Rectangle
	}{
		{left, image.Rect(100, 150, 500, 650), image.Pt(3200, 1080), image.Pt(1280, 720), image.Rect(100, 150, 500, 650)},
		{left, image.Rect(100, 150, 500, 650), image.Pt(3200, 1080), image.Pt(2560, 1440), image.Rect(200, 300, 1000, 1300)},
		{right, image.Rect(1480, 100, 2880, 1000), image.Pt(3200, 1080), image.Pt(1920, 1080), image.Rect(200, 100, 1600, 1000)},
		{right, image.Rect(2960, 200, 5760, 2000), image.Pt(6400, 2160), image.Pt(1920, 1080), image.Rect(200, 100, 1600, 1000)},
	} {
		got, err := DesktopRegionToStream(tc.region, tc.target, displays, tc.desktopSize, tc.sourceSize)
		if err != nil || got != tc.want {
			t.Fatalf("mapped selection %v != %v: %v", got, tc.want, err)
		}
	}
	if _, err := DesktopRegionToStream(image.Rect(1200, 0, 1400, 600), left, displays, image.Pt(3200, 1080), image.Pt(1280, 720)); err == nil {
		t.Fatal("selection crossing monitors was accepted")
	}
	if _, err := DesktopRegionToStream(image.Rect(100, 100, 500, 600), left, displays[:1], image.Pt(1920, 1080), image.Pt(1280, 720)); err == nil {
		t.Fatal("disconnected display was accepted")
	}
}

func TestScreenRegionConfirmsReleaseCoordinatesAndReleasesState(t *testing.T) {
	v := &pickView{display: mygo.Display{Bounds: mygo.Rectangle{Width: 800, Height: 600}}, crop: image.Rect(-1600, 0, 0, 1200)}
	s := &pick{full: image.NewRGBA(v.crop), result: make(chan pickResult, 1), active: v, dragging: true, origin: doc.Point{X: 10, Y: 20}, corner: doc.Point{X: 10, Y: 20}, minimum: image.Pt(80, 160)}
	if !s.input(v, ui.InputEvent{Kind: ui.InputPointerUp, X: 210, Y: 220}, 800, 600) {
		t.Fatal("release not handled")
	}
	choice := <-s.result
	if choice.err != nil || choice.region != image.Rect(-1580, 40, -1180, 440) {
		t.Fatal(choice)
	}
	if !s.done || s.full != nil || s.active != nil || s.dragging {
		t.Fatal("selection retained desktop state")
	}
}

func TestScreenRegionSmallDragDoesNotConfirmAndEscapeCancels(t *testing.T) {
	v := &pickView{display: mygo.Display{Bounds: mygo.Rectangle{Width: 800, Height: 600}}, crop: image.Rect(0, 0, 800, 600)}
	s := &pick{result: make(chan pickResult, 1), active: v, dragging: true, origin: doc.Point{X: 10, Y: 10}, minimum: image.Pt(80, 160)}
	s.input(v, ui.InputEvent{Kind: ui.InputPointerUp, X: 50, Y: 50}, 800, 600)
	if s.done {
		t.Fatal("tiny scroll selection confirmed")
	}
	s.input(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEscape}, 800, 600)
	if choice := <-s.result; choice.err != capture.ErrCancelled {
		t.Fatal(choice)
	}
}
