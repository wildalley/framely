package desktop

import "github.com/egoist/mygo/ui"

type ocrWordHit struct {
	index  int
	handle ui.Handle
}
type ocrSweep struct {
	pressed, dragging, selectOn bool
	first                       int
	shift                       bool
	start, last                 ui.Point
}

// Capture on the list, keeping each chip's native toggle keyboard semantics.
// Handle bounds come from the committed frame, including list scrolling. A
// reusable slot per live chip keeps hit-testing storage bounded by the view.
func (v *ocrWindow) sweepInput(c *ui.Context, ev ui.InputEvent) bool {
	if v.closed || v.tab != 0 || v.selection == nil || ev.Button != 0 {
		return false
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
		v.sweep = ocrSweep{pressed: true, first: i, selectOn: !v.selection.Tokens[i].Selected, shift: ev.Mods&ui.Shift != 0, start: p, last: p}
		return true
	case ui.InputPointerMove, ui.InputPointerUp:
		if !v.sweep.pressed {
			return false
		}
		d := &v.sweep
		dx, dy := p.X-d.start.X, p.Y-d.start.Y
		if !d.dragging && dx*dx+dy*dy >= 16 {
			d.dragging = true
			v.selection.Tokens[d.first].Selected = d.selectOn
			v.anchor = d.first
		}
		if d.dragging {
			for _, hit := range v.hits[:v.hitUsed] {
				r := clipOCRRect(hit.handle.Bounds(c), viewport)
				if hit.index < len(v.selection.Tokens) && segmentHitsRect(d.last, p, r) {
					v.selection.Tokens[hit.index].Selected = d.selectOn
				}
			}
			v.refreshSelection()
		}
		d.last = p
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
