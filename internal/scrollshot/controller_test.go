package scrollshot

import (
	"errors"
	"testing"
	"time"
)

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
