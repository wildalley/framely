package scrollshot

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"
)

func animatedViewport(page *image.RGBA, at, phase int) *image.RGBA {
	f := viewport(page, at, 500, 20, 20)
	// A small inline GIF moves with the document, but changes independently.
	r := image.Rect(40, 220-at, 260, 320-at).Intersect(f.Bounds())
	c := color.RGBA{R: 30, G: 60, B: 220, A: 255}
	if phase%2 != 0 {
		c = color.RGBA{R: 245, G: 210, B: 30, A: 255}
	}
	draw.Draw(f, r, image.NewUniform(c), image.Point{}, draw.Src)
	return f
}

func TestLocalAnimationDoesNotInventScrolling(t *testing.T) {
	page := document(320, 2000)
	s := New()
	if _, err := s.Add(animatedViewport(page, 0, 0)); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 8; i++ {
		out, err := s.Add(animatedViewport(page, 0, i))
		if err != nil || !out.Duplicate || s.Height() != 540 {
			t.Fatalf("animation appended content: %+v %v", out, err)
		}
	}
}

func TestScrollWithInlineAnimationKeepsStaticPixelsAligned(t *testing.T) {
	page := document(320, 2000)
	s := New()
	for i, at := range []int{0, 65, 130, 195, 260} {
		out, err := s.Add(animatedViewport(page, at, i))
		if err != nil || out.Height != 540+at {
			t.Fatalf("animated viewport at %d: %+v %v", at, out, err)
		}
	}
	got := s.Flatten()
	want := viewport(page, 0, 760, 20, 20)
	// The animated area may reflect different captured moments. Text/pixels
	// outside it must keep their exact document position without duplicates.
	for y := range got.Rect.Dy() {
		for _, x := range []int{10, 300} {
			if got.RGBAAt(x, y) != want.RGBAAt(x, y) {
				t.Fatalf("static content shifted at %d,%d", x, y)
			}
		}
	}
}

func TestAutomaticScrollCanSettleDespiteLocalAnimation(t *testing.T) {
	now := time.Unix(1000, 0)
	page := document(320, 2000)
	c, err := NewController(animatedViewport(page, 0, 0), true, now)
	if err != nil {
		t.Fatal(err)
	}
	var scroll bool
	for i := 1; i <= 3; i++ {
		at := now.Add(time.Duration(i) * 100 * time.Millisecond)
		c.Feed(animatedViewport(page, 0, i), at)
		_, scroll = c.Tick(at)
	}
	if !scroll || c.Paused || c.Stitch.Height() != 540 {
		t.Fatal("local animation blocked scrolling or invented new rows")
	}
}

func TestTransientMismatchDoesNotImmediatelyShowWarning(t *testing.T) {
	now := time.Unix(1000, 0)
	page := document(320, 2200)
	c, err := NewController(viewport(page, 0, 500, 20, 20), false, now)
	if err != nil {
		t.Fatal(err)
	}
	c.Feed(viewport(page, 1400, 500, 20, 20), now.Add(time.Second))
	c.Tick(now.Add(1200 * time.Millisecond))
	if c.Notice != "" || c.Paused {
		t.Fatal("one temporary mismatch interrupted capture")
	}
	c.Feed(viewport(page, 65, 500, 20, 20), now.Add(1300*time.Millisecond))
	c.Tick(now.Add(1500 * time.Millisecond))
	if c.Notice != "" || c.Stitch.Height() != 605 {
		t.Fatal("failed to recover without a warning")
	}
}

func TestReturningToAcceptedFrameClearsAlignmentNotice(t *testing.T) {
	now := time.Unix(1000, 0)
	page := document(320, 2200)
	first := viewport(page, 0, 500, 20, 20)
	c, err := NewController(first, false, now)
	if err != nil {
		t.Fatal(err)
	}
	c.Feed(viewport(page, 1400, 500, 20, 20), now.Add(time.Second))
	c.Tick(now.Add(1200 * time.Millisecond))
	c.Tick(now.Add(2 * time.Second))
	if c.Notice == "" {
		t.Fatal("persistent mismatch was not reported")
	}
	c.Feed(first, now.Add(3*time.Second))
	changed, _ := c.Tick(now.Add(3200 * time.Millisecond))
	if !changed || c.Notice != "" || c.Paused || c.Stitch.Height() != 540 {
		t.Fatal("recovered frame retained stale warning")
	}
}
