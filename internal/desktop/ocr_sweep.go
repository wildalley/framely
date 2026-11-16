package desktop

import (
	"github.com/egoist/mygo/ui"
	"time"
)

type ocrWordHit struct {
	index  int
	handle ui.Handle
}
type ocrSweep struct {
	pressed, dragging, selectOn bool
	first                       int
	shift                       bool
	start, last                 ui.Point
	previousAnchor              int
	changed                     []int
	tick                        time.Time
	scrollY                     float32
}

// Capture on the list, keeping each chip's native toggle keyboard semantics.
// Handle bounds come from the committed frame, including list scrolling. A
// reusable slot per live chip keeps hit-testing storage bounded by the view.
func (v *ocrWindow) sweepInput(c *ui.Context, ev ui.InputEvent) bool {
	if v.closed || v.tab != 0 || v.selection == nil || ev.Button != 0 {
		return false
	}
	if ev.Kind == ui.InputPointerUp && ev.Canceled {
		if !v.sweep.pressed {
			return false
		}
		for _, i := range v.sweep.changed {
			v.selection.Tokens[i].Selected = !v.sweep.selectOn
		}
		v.anchor = v.sweep.previousAnchor
		v.sweep = ocrSweep{}
		v.refreshSelection()
		return true
	}
	viewport := v.list.Bounds(c)
	p := ui.Point{X: viewport.X + ev.X, Y: viewport.Y + ev.Y}
	hitAt := func(p ui.Point) int {
		if !pointIn(viewport, p) {
			return -1
		}
		for _, hit := range v.hits[:v.hitUsed] {
			if hit.index < len(v.selection.Tokens) && pointIn(hit.handle.Bounds(c), p) {
				return hit.index
			}
		}
		return -1
	}
	switch ev.Kind {
	case ui.InputPointerDown:
		i := hitAt(p)
		if i < 0 {
			return false
		}
		v.sweep = ocrSweep{pressed: true, first: i, selectOn: !v.selection.Tokens[i].Selected, shift: ev.Mods&ui.Shift != 0, start: p, last: p, previousAnchor: v.anchor}
		v.sweep.scrollY = v.wordScroll.Y
		return true
	case ui.InputPointerMove, ui.InputPointerUp:
		if !v.sweep.pressed {
			return false
		}
		d := &v.sweep
		selectWord := func(i int) {
			if v.selection.Tokens[i].Selected != d.selectOn {
				d.changed = append(d.changed, i)
				v.selection.Tokens[i].Selected = d.selectOn
			}
		}
		dx, dy := p.X-d.start.X, p.Y-d.start.Y
		if !d.dragging && dx*dx+dy*dy >= 16 {
			d.dragging = true
			selectWord(d.first)
			v.anchor = d.first
		}
		if d.dragging {
			for _, hit := range v.hits[:v.hitUsed] {
				r := clipOCRRect(hit.handle.Bounds(c), viewport)
				if hit.index < len(v.selection.Tokens) && segmentHitsRect(d.last, p, r) {
					selectWord(hit.index)
				}
			}
			v.refreshSelection()
		}
		d.last = p
		d.tick = c.Now()
		if d.dragging && sweepVelocity(viewport, p) != 0 {
			c.AnimationFrame()
		}
		if ev.Kind == ui.InputPointerUp {
			if !d.dragging && hitAt(p) == d.first {
				v.selection.Tokens[d.first].Selected = d.selectOn
				if d.shift && v.anchor >= 0 {
					v.selection.Range(v.anchor, d.first, d.selectOn)
				}
				v.anchor = d.first
				v.refreshSelection()
			}
			v.sweep = ocrSweep{}
		}
		return true
	}
	return false
}

// Each requested frame uses committed chip bounds before rebuilding the list.
// No timer or worker survives release, tab changes, replacement or closing.
func (v *ocrWindow) sweepFrame(c *ui.Context) {
	d := &v.sweep
	if v.closed || v.tab != 0 || !d.pressed || !d.dragging {
		return
	}
	viewport := v.list.Bounds(c)
	velocity := sweepVelocity(viewport, d.last)
	elapsed := min(c.Now().Sub(d.tick).Seconds(), .05)
	// A consumed input can rebuild the view before committing its layout.
	// Process geometry once per display time, never from that interim pass.
	if elapsed <= 0 {
		if velocity != 0 && (velocity < 0 && v.wordScroll.Y > 0 || velocity > 0 && v.wordScroll.Y < v.wordScroll.MaxY) {
			c.AnimationFrame()
		}
		return
	}
	delta := v.wordScroll.Y - d.scrollY
	if delta != 0 {
		changed := false
		p := d.last
		p.Y = max(viewport.Y+.5, min(p.Y, viewport.Y+viewport.H-.5))
		a := p
		a.Y -= delta
		for _, hit := range v.hits[:v.hitUsed] {
			if hit.index >= len(v.selection.Tokens) || !segmentHitsRect(a, p, clipOCRRect(hit.handle.Bounds(c), viewport)) {
				continue
			}
			if v.selection.Tokens[hit.index].Selected != d.selectOn {
				d.changed = append(d.changed, hit.index)
				v.selection.Tokens[hit.index].Selected = d.selectOn
				changed = true
			}
		}
		if changed {
			v.refreshSelection()
		}
	}
	d.scrollY = v.wordScroll.Y
	d.tick = c.Now()
	if velocity == 0 || velocity < 0 && v.wordScroll.Y <= 0 || velocity > 0 && v.wordScroll.Y >= v.wordScroll.MaxY {
		return
	}
	c.AnimationFrame()
	next := max(float32(0), min(v.wordScroll.MaxY, v.wordScroll.Y+velocity*float32(elapsed)))
	if next != v.wordScroll.Y {
		v.wordScroll.Y = next
	}
}

func sweepVelocity(r ui.Rect, p ui.Point) float32 {
	if r.W <= 0 || r.H <= 0 || p.X < r.X || p.X >= r.X+r.W {
		return 0
	}
	edge := min(float32(28), r.H/3)
	if p.Y < r.Y+edge {
		return -420 * min(float32(1), (r.Y+edge-p.Y)/edge)
	}
	if p.Y > r.Y+r.H-edge {
		return 420 * min(float32(1), (p.Y-(r.Y+r.H-edge))/edge)
	}
	return 0
}

func pointIn(r ui.Rect, p ui.Point) bool {
	return r.W > 0 && r.H > 0 && p.X >= r.X && p.X < r.X+r.W && p.Y >= r.Y && p.Y < r.Y+r.H
}
func clipOCRRect(a, b ui.Rect) ui.Rect {
	x, y := max(a.X, b.X), max(a.Y, b.Y)
	return ui.Rect{X: x, Y: y, W: max(0, min(a.X+a.W, b.X+b.W)-x), H: max(0, min(a.Y+a.H, b.Y+b.H)-y)}
}

// Clip the pointer segment against each chip so fast motion cannot skip words
// between event samples, while movement through empty gaps selects nothing.
func segmentHitsRect(a, b ui.Point, r ui.Rect) bool {
	if r.W <= 0 || r.H <= 0 {
		return false
	}
	lo, hi := float32(0), float32(1)
	for _, axis := range [][4]float32{{a.X, b.X - a.X, r.X, r.X + r.W}, {a.Y, b.Y - a.Y, r.Y, r.Y + r.H}} {
		if axis[1] == 0 {
			if axis[0] < axis[2] || axis[0] >= axis[3] {
				return false
			}
			continue
		}
		u, v := (axis[2]-axis[0])/axis[1], (axis[3]-axis[0])/axis[1]
		if u > v {
			u, v = v, u
		}
		lo, hi = max(lo, u), min(hi, v)
		if lo > hi {
			return false
		}
	}
	return true
}
