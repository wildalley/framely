package scrollshot

import (
	"image"
	"time"
)

// Controller owns all stitch state on its worker goroutine. Tick is independent
// of frame arrival, since a compositor may only send frames when pixels change.
type Controller struct {
	Stitch                                           *Stitcher
	Automatic, Paused                                bool
	Notice                                           string
	candidate                                        *image.RGBA
	candidateAt, ignoreUntil, lastScroll, nextScroll time.Time
	waiting                                          bool
	unchanged                                        int
}

func NewController(first *image.RGBA, automatic bool, now time.Time) (*Controller, error) {
	s := New()
	if _, err := s.Add(first); err != nil {
		return nil, err
	}
	return &Controller{Stitch: s, Automatic: automatic, candidate: first, candidateAt: now, ignoreUntil: now.Add(350 * time.Millisecond), nextScroll: now.Add(550 * time.Millisecond)}, nil
}
func (c *Controller) Feed(frame *image.RGBA, now time.Time) {
	if c.Paused {
		return
	}
	if Difference(c.candidate, frame) > 0.7 {
		c.candidateAt = now
	}
	c.candidate = frame
}

// Tick returns whether the UI changed and whether one scroll step is needed.
func (c *Controller) Tick(now time.Time) (changed, scroll bool) {
	if c.Paused || c.candidate == nil || now.Before(c.ignoreUntil) {
		return false, false
	}
	if now.Sub(c.candidateAt) < 180*time.Millisecond {
		if c.Automatic && c.waiting && now.Sub(c.lastScroll) > 5*time.Second {
			c.Pause("页面持续变化，已暂停自动滚动；请避开动画区域或使用手动模式。")
			return true, false
		}
		return false, false
	}
	out, err := c.Stitch.Add(c.candidate)
	if err != nil {
		c.Pause(err.Error())
		return true, false
	}
	if out.Added > 0 {
		c.waiting = false
		c.unchanged = 0
		c.nextScroll = now.Add(250 * time.Millisecond)
		c.Notice = ""
		changed = true
	}
	if !c.Automatic || now.Before(c.nextScroll) {
		return changed, false
	}
	if c.waiting {
		if now.Sub(c.lastScroll) < 850*time.Millisecond {
			return changed, false
		}
		c.unchanged++
		if c.unchanged >= 3 {
			c.Pause("画面没有继续变化，自动滚动已暂停。确认到底后可完成截图，也可继续手动滚动。")
			return true, false
		}
	}
	return changed, true
}
func (c *Controller) Scrolled(now time.Time, err error) {
	if err != nil {
		c.Pause("自动滚动已暂停：" + err.Error() + "。可改用手动模式继续。")
		return
	}
	c.waiting = true
	c.lastScroll = now
	c.nextScroll = now.Add(850 * time.Millisecond)
	c.ignoreUntil = now.Add(180 * time.Millisecond)
}
func (c *Controller) Pause(notice string) { c.Paused = true; c.Notice = notice; c.waiting = false }
func (c *Controller) Resume(automatic bool, now time.Time) {
	c.Paused = false
	c.Automatic = automatic
	c.Notice = ""
	c.waiting = false
	c.unchanged = 0
	c.ignoreUntil = now.Add(350 * time.Millisecond)
	c.nextScroll = now.Add(550 * time.Millisecond)
	c.candidateAt = now
}
func (c *Controller) Undo() {
	if c.Stitch.Undo() {
		c.candidate = c.Stitch.previous
		c.Automatic = false
		c.Pause("已撤回最后一段。请手动滚回上一段末尾，再继续截图。")
	}
}
