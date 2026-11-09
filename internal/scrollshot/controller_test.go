package scrollshot

import (
	"errors"
	"image"
	"testing"
	"time"
)

func TestManualDragStitchesBeforeMotionStops(t *testing.T) {
	now := time.Unix(1000, 0)
	page := document(180, 5000)
	c, err := NewController(viewport(page, 0, 500, 20, 16), false, now)
	if err != nil {
		t.Fatal(err)
	}
	// Ten seconds of uninterrupted scrollbar dragging, with a new frame
	// every 40 ms. Waiting for motion to stop would lose all initial overlap.
	for i := 1; i <= 250; i++ {
		at := now.Add(time.Duration(200+i*40) * time.Millisecond)
		c.Feed(viewport(page, i*12, 500, 20, 16), at)
		c.Tick(at)
	}
	c.Tick(now.Add(11 * time.Second))
	if c.Paused || c.Stitch.Height() != 3536 {
		t.Fatalf("paused=%v height=%d notice=%s", c.Paused, c.Stitch.Height(), c.Notice)
	}
	want := viewport(page, 0, 3500, 20, 16)
	got := c.Stitch.Flatten()
	if Difference(got, want) != 0 {
		t.Fatal("continuous drag lost or repeated rows")
	}
}

func TestManualUncertainFrameCanRecoverWithoutResumeClick(t *testing.T) {
	now := time.Unix(1000, 0)
	page := document(180, 3000)
	c, err := NewController(viewport(page, 0, 500, 0, 0), false, now)
	if err != nil {
		t.Fatal(err)
	}
	c.Feed(viewport(page, 1800, 500, 0, 0), now.Add(time.Second))
	c.Tick(now.Add(2 * time.Second))
	if c.Paused || c.Notice == "" || c.Stitch.Height() != 500 {
		t.Fatal("unmatched manual frame stopped capture or damaged the result")
	}
	c.Feed(viewport(page, 75, 500, 0, 0), now.Add(3*time.Second))
	c.Tick(now.Add(4 * time.Second))
	if c.Paused || c.Notice != "" || c.Stitch.Height() != 575 {
		t.Fatalf("manual recovery: paused=%v height=%d notice=%s", c.Paused, c.Stitch.Height(), c.Notice)
	}
}

func BenchmarkManualMovingViewport(b *testing.B) {
	page := document(1358, 2400)
	first := page.SubImage(image.Rect(0, 0, 1358, 1074)).(*image.RGBA)
	second := page.SubImage(image.Rect(0, 35, 1358, 1109)).(*image.RGBA)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := NewController(first, false, time.Unix(0, 0))
		c.Feed(second, time.Unix(1, 0))
		c.Tick(time.Unix(2, 0))
	}
}

func TestAutomaticScrollWaitsForSettledFramesAndStopsAfterNoProgress(t *testing.T) {
	now := time.Unix(1000, 0)
	doc := document(160, 1000)
	c, err := NewController(viewport(doc, 0, 240, 20, 16), true, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, scroll := c.Tick(now.Add(100 * time.Millisecond)); scroll {
		t.Fatal("scrolled before source selection settled")
	}
	if _, scroll := c.Tick(now.Add(600 * time.Millisecond)); !scroll {
		t.Fatal("automatic mode never requested a scroll")
	}
	c.Scrolled(now.Add(600*time.Millisecond), nil)
	for i := 1; i <= 3; i++ {
		at := now.Add(time.Duration(600+900*i) * time.Millisecond)
		_, scroll := c.Tick(at)
		if i < 3 {
			if !scroll {
				t.Fatal("did not retry unchanged scroll")
			}
			c.Scrolled(at, nil)
		} else if scroll || !c.Paused {
			t.Fatal("continued scrolling indefinitely at end of content")
		}
	}
	if c.Stitch.Height() != 276 {
		t.Fatal("duplicated an unchanged viewport")
	}
}

func TestInputFailurePausesButAllowsManualContinuation(t *testing.T) {
	now := time.Unix(1000, 0)
	doc := document(160, 1000)
	c, err := NewController(viewport(doc, 0, 240, 20, 16), true, now)
	if err != nil {
		t.Fatal(err)
	}
	c.Scrolled(now, errors.New("input permission denied"))
	if !c.Paused || c.Notice == "" {
		t.Fatal("input failure was invisible")
	}
	c.Resume(false, now.Add(time.Second))
	c.Feed(viewport(doc, 45, 240, 20, 16), now.Add(1500*time.Millisecond))
	changed, scroll := c.Tick(now.Add(1800 * time.Millisecond))
	if !changed || scroll || c.Stitch.Height() != 321 {
		t.Fatalf("manual recovery changed=%v scroll=%v height=%d", changed, scroll, c.Stitch.Height())
	}
	c.Undo()
	if !c.Paused || c.Automatic || c.Stitch.Height() != 276 {
		t.Fatal("undo did not pause and preserve prior result")
	}
}

func TestChangingFramesNeverTriggersBlindAutomaticScrolling(t *testing.T) {
	now := time.Unix(1000, 0)
	doc := document(160, 1000)
	c, err := NewController(viewport(doc, 0, 240, 20, 16), true, now)
	if err != nil {
		t.Fatal(err)
	}
	c.Scrolled(now, nil)
	for i := 1; i <= 60; i++ {
		at := now.Add(time.Duration(i) * 100 * time.Millisecond)
		frame := viewport(doc, i, 240, 20, 16)
		c.Feed(frame, at)
		_, scroll := c.Tick(at)
		if scroll {
			t.Fatal("scrolled while animation was not settled")
		}
	}
	if !c.Paused || c.Stitch.Height() != 276 {
		t.Fatal("continuously changing content was appended or never paused")
	}
}

func TestSettledProgressAllowsNextScrollWithoutFixedLongDelay(t *testing.T) {
	now := time.Unix(1000, 0)
	page := document(160, 1000)
	c, err := NewController(viewport(page, 0, 240, 20, 16), true, now)
	if err != nil {
		t.Fatal(err)
	}
	c.Scrolled(now.Add(250*time.Millisecond), nil)
	c.Feed(viewport(page, 45, 240, 20, 16), now.Add(300*time.Millisecond))
	if changed, scroll := c.Tick(now.Add(430 * time.Millisecond)); !changed || scroll {
		t.Fatalf("settled viewport changed=%v scroll=%v", changed, scroll)
	}
	if _, scroll := c.Tick(now.Add(520 * time.Millisecond)); !scroll {
		t.Fatal("waited unnecessarily after accepting stable progress")
	}
	if c.Stitch.Height() != 321 {
		t.Fatal(c.Stitch.Height())
	}
}

func BenchmarkUnchangedViewportController(b *testing.B) {
	frame := document(1920, 1080)
	now := time.Unix(1000, 0)
	c, err := NewController(frame, false, now)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Feed(frame, now.Add(time.Second))
		c.Tick(now.Add(2 * time.Second))
	}
}
