package editor

import (
	"math"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/doc"
	"github.com/wildalley/screenpin/internal/render"
)

type dragKind int

const (
	dragDraw dragKind = iota
	dragMove
	dragResize
)

// drag is a press on the canvas that has not been released yet.
type drag struct {
	kind     dragKind
	id       string
	start    doc.Point
	original doc.Annotation
	handle   doc.Handle
}

// handleRadius is the size of the selection's handles on screen, whatever
// the zoom, so they stay easy to grab on large images.
const handleRadius = 7

// beginGesture opens the transaction of a drag, with a list of its own
// that the drag changes in place (see doc.History).
func (a *App) beginGesture(list []doc.Annotation) {
	a.history.Begin()
	a.history.SetTransient(slices.Clone(list))
}

// working returns the annotation being dragged, in the drag's own list.
func (a *App) working() *doc.Annotation {
	list := a.history.Present()
	if i := doc.IndexOf(list, a.drag.id); i >= 0 {
		return &list[i]
	}
	return nil
}

func (a *App) selectedAnnotation() *doc.Annotation {
	list := a.history.Present()
	if i := doc.IndexOf(list, a.selected); i >= 0 {
		return &list[i]
	}
	return nil
}

func (a *App) toImage(x, y float32, zoom float64) doc.Point {
	return doc.Point{X: float64(x) / zoom, Y: float64(y) / zoom}
}

// handles are the selection's handles at their place on the canvas, in
// canvas DIPs.
func handles(an *doc.Annotation, zoom float64) map[doc.Handle]doc.Point {
	if an.Type == doc.ToolArrow {
		e := an.End()
		return map[doc.Handle]doc.Point{
			doc.HandleStart: {X: an.X * zoom, Y: an.Y * zoom},
			doc.HandleEnd:   {X: e.X * zoom, Y: e.Y * zoom},
		}
	}
	b := an.Bounds()
	return rectHandles(b, zoom)
}

func rectHandles(b doc.Rect, zoom float64) map[doc.Handle]doc.Point {
	return map[doc.Handle]doc.Point{
		doc.HandleNW: {X: b.X * zoom, Y: b.Y * zoom},
		doc.HandleNE: {X: (b.X + b.W) * zoom, Y: b.Y * zoom},
		doc.HandleSW: {X: b.X * zoom, Y: (b.Y + b.H) * zoom},
		doc.HandleSE: {X: (b.X + b.W) * zoom, Y: (b.Y + b.H) * zoom},
		doc.HandleN:  {X: (b.X + b.W/2) * zoom, Y: b.Y * zoom},
		doc.HandleS:  {X: (b.X + b.W/2) * zoom, Y: (b.Y + b.H) * zoom},
		doc.HandleW:  {X: b.X * zoom, Y: (b.Y + b.H/2) * zoom},
		doc.HandleE:  {X: (b.X + b.W) * zoom, Y: (b.Y + b.H/2) * zoom},
	}
}

// handleAt returns the selection's handle under the pointer, if any.
func (a *App) handleAt(x, y float32, zoom float64) (doc.Handle, bool) {
	sel := a.selectedAnnotation()
	if sel == nil {
		return "", false
	}
	closest, distance := doc.Handle(""), float64(handleRadius+3)
	for h, p := range handles(sel, zoom) {
		d := math.Hypot(float64(x)-p.X, float64(y)-p.Y)
		if d < distance || d == distance && (closest == "" || h < closest) {
			closest, distance = h, d
		}
	}
	return closest, closest != ""
}

func handleCursor(h doc.Handle) ui.Cursor {
	switch h {
	case doc.HandleNW, doc.HandleSE:
		return ui.CursorResizeNWSE
	case doc.HandleNE, doc.HandleSW:
		return ui.CursorResizeNESW
	case doc.HandleN, doc.HandleS:
		return ui.CursorResizeNS
	case doc.HandleW, doc.HandleE:
		return ui.CursorResizeEW
	}
	return ui.CursorMove
}

// canvasInput handles the pointer on the canvas. A press it leaves, on the
// empty image with the select tool, drags the window, as a pinned note does.
func (a *App) canvasInput(ev ui.InputEvent, zoom float64) bool {
	if a.busy {
		return true
	}
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false // MyGo opens the native context menu.
		}
		a.finishEditing()
		return a.pointerDown(ev, zoom)
	case ui.InputPointerMove:
		if a.drag != nil {
			a.dragTo(a.toImage(ev.X, ev.Y, zoom))
			return true
		}
		a.hoverCursor = a.cursorAt(ev.X, ev.Y, zoom)
		return false
	case ui.InputPointerUp:
		if a.drag != nil && ev.Button == 0 {
			if ev.Canceled {
				a.history.Cancel()
				a.drag = nil
				if doc.IndexOf(a.history.Present(), a.selected) < 0 {
					a.selected = ""
				}
				return true
			}
			a.dragTo(a.toImage(ev.X, ev.Y, zoom))
			a.finishDrag()
			return true
		}
	case ui.InputScroll:
		if ev.Mods&ui.Cmd != 0 && ev.DY != 0 {
			if ev.DY < 0 {
				a.setZoom(a.displayZoom * 1.1)
			} else {
				a.setZoom(a.displayZoom / 1.1)
			}
			return true
		}
	case ui.InputZoom:
		a.setZoom(a.displayZoom * float64(ev.Scale))
		return true
	}
	return false
}

func (a *App) pointerDown(ev ui.InputEvent, zoom float64) bool {
	p := a.toImage(ev.X, ev.Y, zoom)
	list := a.history.Present()
	if a.tool == doc.ToolSelect {
		if h, ok := a.handleAt(ev.X, ev.Y, zoom); ok {
			sel := a.selectedAnnotation()
			a.drag = &drag{kind: dragResize, id: sel.ID, start: p, original: sel.Clone(), handle: h}
			a.beginGesture(list)
			return true
		}
		if i := doc.HitTest(list, p, 4/zoom); i >= 0 {
			an := list[i]
			if a.selected != an.ID {
				a.propertyImage = false
			}
			a.selected = an.ID
			if ev.Clicks >= 2 && an.Type == doc.ToolText {
				a.startEditing(an.ID)
				return true
			}
			a.drag = &drag{kind: dragMove, id: an.ID, start: p, original: an.Clone()}
			a.beginGesture(list)
			return true
		}
		a.selected = ""
		if a.panel == PanelStyle {
			a.panel = PanelNone
		}
		// A double click on the empty image would maximize the window.
		return ev.Clicks >= 2
	}
	if a.tool == doc.ToolMosaic && a.mosaic == nil {
		return true
	}
	an := doc.New(a.tool, p, a.styles[a.tool])
	a.beginGesture(append(slices.Clone(list), an))
	a.drag = &drag{kind: dragDraw, id: an.ID, start: p}
	// Shapes stay selected to be adjusted right away. Strokes are drawn in
	// runs: a selection would flash around each, and the style panel would
	// restyle the last stroke instead of the next.
	if a.tool.Freehand() {
		a.selected = ""
	} else {
		a.selected = an.ID
	}
	return true
}

func (a *App) dragTo(p doc.Point) {
	w := a.working()
	if w == nil {
		return
	}
	d := a.drag
	dx, dy := p.X-d.start.X, p.Y-d.start.Y
	switch d.kind {
	case dragMove:
		*w = d.original.Moved(dx, dy)
	case dragResize:
		*w = d.original.Resized(d.handle, dx, dy)
	case dragDraw:
		switch {
		case w.Type.Freehand():
			if last := w.Points[len(w.Points)-1]; last != p {
				w.Points = append(w.Points, p)
			}
		case w.Type == doc.ToolArrow:
			w.SetEnd(p)
			w.Width, w.Height = dx, dy
		case w.Type == doc.ToolText:
		default:
			w.Width, w.Height = dx, dy
		}
	}
}

func (a *App) finishDrag() {
	d := a.drag
	a.drag = nil
	list := a.history.Present()
	i := doc.IndexOf(list, d.id)
	var created *doc.Annotation
	if i >= 0 {
		if list[i].Type.Boxed() {
			list[i] = list[i].Normalized()
		}
		if d.kind == dragDraw && list[i].TooSmall() {
			a.history.SetTransient(slices.Delete(list, i, i+1))
			a.selected = ""
		} else if d.kind == dragDraw {
			created = &list[i]
		}
	}
	a.history.End()
	if created != nil && !created.Type.Freehand() {
		a.propertyImage = false
		a.tool = doc.ToolSelect
		a.hoverCursor = ui.CursorDefault
	}
	if created != nil && created.Type == doc.ToolText {
		a.tool = doc.ToolSelect
		a.startEditing(created.ID)
	}
}

func (a *App) cursorAt(x, y float32, zoom float64) ui.Cursor {
	if a.tool != doc.ToolSelect {
		if a.tool == doc.ToolMosaic && a.mosaic == nil {
			return ui.CursorNotAllowed
		}
		return ui.CursorCrosshair
	}
	if h, ok := a.handleAt(x, y, zoom); ok {
		return handleCursor(h)
	}
	if doc.HitTest(a.history.Present(), a.toImage(x, y, zoom), 4/zoom) >= 0 {
		return ui.CursorMove
	}
	return ui.CursorGrab
}

// startEditing edits the text annotation id in place.
func (a *App) startEditing(id string) {
	a.finishEditing()
	list := a.history.Present()
	i := doc.IndexOf(list, id)
	if i < 0 || list[i].Type != doc.ToolText {
		return
	}
	a.selected = id
	a.editingID = id
	a.editingText = list[i].TextValue()
	a.editFresh = true
	a.history.Begin()
}

// finishEditing ends the text edit, as one step of the history.
func (a *App) finishEditing() {
	if a.drag != nil {
		a.finishDrag()
	}
	if a.textEdit || a.slidingKey != "" {
		a.textEdit, a.slidingKey = false, ""
		a.history.End()
	}
	if a.propertyEditKey != "" {
		a.propertyEditKey = ""
		a.history.End()
	}
	if a.editingID == "" {
		return
	}
	a.selected = a.editingID
	a.editingID = ""
	a.history.End()
}

func (a *App) setEditingText(s string) {
	list := a.history.Present()
	i := doc.IndexOf(list, a.editingID)
	if i < 0 {
		return
	}
	an := list[i].Clone()
	an.SetText(s)
	a.history.SetTransient(doc.Replace(list, i, an))
}

// canvas builds the image and its annotations at the display zoom.
func (a *App) canvas(c *ui.Context, zoom float64) ui.Element {
	w := float32(float64(a.image.Width) * zoom)
	h := float32(float64(a.image.Height) * zoom)
	scene := &render.Scene{
		Image:       a.bitmap,
		Mosaic:      a.mosaic,
		Width:       a.image.Width,
		Height:      a.image.Height,
		Annotations: a.history.Present(),
		Hidden:      a.editingID,
		CornerRatio: a.history.ImageCornerRatio(),
	}
	box := ui.Box(c.Key("canvas")).Label("图片画布").Size(w, h).Cursor(a.hoverCursor).ContextMenu(a.contextMenu)
	if a.drag != nil {
		if a.drag.kind == dragResize {
			box.Cursor(handleCursor(a.drag.handle))
		} else if a.drag.kind == dragMove {
			box.Cursor(ui.CursorMove)
		}
	}
	if a.tool == doc.ToolSelect && a.drag == nil {
		box.DragWindow()
	}
	box.HandleInput(func(ev ui.InputEvent) bool { return a.canvasInput(ev, zoom) })
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		z := float32(zoom)
		p.Clip(r, 0, func() {
			p.Transform(ui.Scaling(z, z).Then(ui.Translation(r.X, r.Y)), 0, 0, func() {
				render.Draw(p, scene)
			})
			if sel := a.selectedAnnotation(); sel != nil && a.editingID == "" {
				drawSelection(p, r, sel, zoom, p.Theme().Accent)
			}
		})
	})
	box.Children(func() {
		if a.editingID != "" {
			a.textEditor(c, zoom)
		}
	})
	return box
}

func drawSelection(p *ui.Painter, r ui.Rect, an *doc.Annotation, zoom float64, accent ui.Color) {
	if an.Type != doc.ToolArrow {
		b := an.Bounds()
		outline := (&ui.Path{}).
			MoveTo(r.X+float32(b.X*zoom), r.Y+float32(b.Y*zoom)).
			LineTo(r.X+float32((b.X+b.W)*zoom), r.Y+float32(b.Y*zoom)).
			LineTo(r.X+float32((b.X+b.W)*zoom), r.Y+float32((b.Y+b.H)*zoom)).
			LineTo(r.X+float32(b.X*zoom), r.Y+float32((b.Y+b.H)*zoom)).
			Close().Cap(ui.ButtCap).Dash(0, 6, 4)
		p.StrokePath(outline, 2, accent)
	}
	for _, h := range handles(an, zoom) {
		cx, cy := r.X+float32(h.X), r.Y+float32(h.Y)
		circle := (&ui.Path{}).Circle(cx, cy, handleRadius)
		p.FillPath(circle, ui.RGBA(255, 255, 255, 1))
		p.StrokePath(circle, 3, accent)
	}
}

// textEditor edits the text annotation in place, over where it shows.
func (a *App) textEditor(c *ui.Context, zoom float64) {
	list := a.history.Present()
	i := doc.IndexOf(list, a.editingID)
	if i < 0 {
		a.editingID = ""
		return
	}
	an := list[i]
	b := an.Bounds()
	z := float32(zoom)
	size := float32(an.FontSize) * z
	area := ui.TextAreaBase(c.Key("text-edit"), &a.editingText).Bind(&a.annotationText).Label("编辑标注文字").
		Absolute().Left(float32(an.X)*z).Top(float32(an.Y)*z).
		Width(max(180*z, float32(b.W)*z+size*2)).
		MinHeight(max(48*z, float32(b.H)*z)).
		FontSize(size).FontWeight(600).LineHeight(doc.LineHeight).
		TextColor(render.Color(an.Color, an.Opacity)).
		Background(ui.RGBA(255, 255, 255, 0.12)).
		Border(1, c.Theme().Accent).
		AutoFocus()
	if render.TextFont != "" {
		area.Font(render.TextFont)
	}
	if a.editFresh {
		a.editFresh = false
		area.SetTextSelection(0, len([]rune(a.editingText)))
	}
	if area.Changed() {
		a.setEditingText(a.editingText)
	}
	if area.Shortcut(0, ui.KeyEscape) || area.Shortcut(ui.Cmd, ui.KeyEnter) || !area.Focused() {
		a.finishEditing()
	}
}
