package editor

import (
	"context"
	"errors"
	"image"
	"math"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/doc"
	"github.com/wildalley/screenpin/internal/render"
)

type regionChoice struct {
	rect image.Rectangle
	err  error
}
type imagePicker struct {
	win            *mygo.Window
	bitmap         *ui.Bitmap
	bounds         image.Rectangle
	imageRect      ui.Rect
	start, end     image.Point
	dragging, done bool
	result         chan regionChoice
	title          string
	mode           dragKind
	handle         doc.Handle
	dragStart      image.Point
	original       image.Rectangle
	cursor         ui.Cursor
}

// SelectImageRegion chooses pixel coordinates from an immutable frame. The
// letterbox, UI scaling and high-DPI display do not change its source pixels.
func SelectImageRegion(ctx context.Context, img *image.RGBA, title string) (image.Rectangle, error) {
	if err := ctx.Err(); err != nil {
		return image.Rectangle{}, err
	}
	if img == nil || img.Bounds().Empty() {
		return image.Rectangle{}, errors.New("无法选择空图片")
	}
	p := &imagePicker{bounds: image.Rect(0, 0, img.Rect.Dx(), img.Rect.Dy()), result: make(chan regionChoice, 1), title: title}
	mygo.RunOnMain(func() {
		if ctx.Err() != nil {
			return
		}
		p.bitmap = ui.NewBitmap(render.Preview(img, 4096))
		display := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
		w, h := min(1100, max(480, img.Rect.Dx())), min(800, max(320, img.Rect.Dy()+100))
		w, h = min(w, max(320, display.WorkArea.Width-40)), min(h, max(240, display.WorkArea.Height-60))
		p.win = mygo.NewWindow(mygo.WindowOptions{Title: title, Width: w, Height: h, MinWidth: 320, MinHeight: 240, UseContentSize: true, Content: ui.View(p.view)})
		p.win.OnClose(func(*mygo.CloseEvent) { p.finish(image.Rectangle{}) })
		p.win.OnClosed(func() { p.finish(image.Rectangle{}) })
	})
	stop := context.AfterFunc(ctx, func() { mygo.RunOnMain(func() { p.finish(image.Rectangle{}) }) })
	defer stop()
	select {
	case <-ctx.Done():
		return image.Rectangle{}, ctx.Err()
	case choice := <-p.result:
		return choice.rect, choice.err
	}
}

func (p *imagePicker) selection() image.Rectangle {
	return image.Rect(min(p.start.X, p.end.X), min(p.start.Y, p.end.Y), max(p.start.X, p.end.X), max(p.start.Y, p.end.Y))
}
func (p *imagePicker) finish(rect image.Rectangle) {
	if p.done {
		return
	}
	p.done = true
	p.bitmap = nil
	if p.win != nil && !p.win.IsDestroyed() {
		p.win.Destroy()
	}
	choice := regionChoice{rect: rect}
	if rect.Empty() {
		choice.err = capture.ErrCancelled
	}
	p.result <- choice
}
func (p *imagePicker) point(x, y float32) image.Point {
	r := p.imageRect
	return image.Pt(int(clamp(float64((x-r.X)/max(1, r.W))*float64(p.bounds.Dx()), 0, float64(p.bounds.Dx()))), int(clamp(float64((y-r.Y)/max(1, r.H))*float64(p.bounds.Dy()), 0, float64(p.bounds.Dy()))))
}

func (p *imagePicker) handles() map[doc.Handle]doc.Point {
	r := p.selection()
	if r.Empty() {
		return nil
	}
	scale := float64(p.imageRect.W) / float64(p.bounds.Dx())
	out := rectHandles(doc.Rect{X: float64(r.Min.X), Y: float64(r.Min.Y), W: float64(r.Dx()), H: float64(r.Dy())}, scale)
	for h, v := range out {
		v.X += float64(p.imageRect.X)
		v.Y += float64(p.imageRect.Y)
		out[h] = v
	}
	return out
}
func (p *imagePicker) handleAt(x, y float32) (doc.Handle, bool) {
	closest, distance := doc.Handle(""), float64(handleRadius+3)
	for h, v := range p.handles() {
		d := math.Hypot(float64(x)-v.X, float64(y)-v.Y)
		if d < distance || d == distance && (closest == "" || h < closest) {
			closest, distance = h, d
		}
	}
	return closest, closest != ""
}
func (p *imagePicker) dragTo(at image.Point) {
	dx, dy := at.X-p.dragStart.X, at.Y-p.dragStart.Y
	r := p.original
	switch p.mode {
	case dragMove:
		dx = max(p.bounds.Min.X-r.Min.X, min(dx, p.bounds.Max.X-r.Max.X))
		dy = max(p.bounds.Min.Y-r.Min.Y, min(dy, p.bounds.Max.Y-r.Max.Y))
		r = r.Add(image.Pt(dx, dy))
	case dragResize:
		x0, y0, x1, y1 := r.Min.X, r.Min.Y, r.Max.X, r.Max.Y
		switch p.handle {
		case doc.HandleNW, doc.HandleNE, doc.HandleN:
			y0 += dy
		case doc.HandleSW, doc.HandleSE, doc.HandleS:
			y1 += dy
		}
		switch p.handle {
		case doc.HandleNW, doc.HandleSW, doc.HandleW:
			x0 += dx
		case doc.HandleNE, doc.HandleSE, doc.HandleE:
			x1 += dx
		}
		r = image.Rect(min(x0, x1), min(y0, y1), max(x0, x1), max(y0, y1))
	default:
		p.end = at
		return
	}
	p.start, p.end = r.Min, r.Max
}

func (p *imagePicker) adjust(dx, dy int, resize bool) {
	r := p.selection()
	if r.Empty() {
		return
	}
	if resize {
		r.Max.X = max(r.Min.X+1, min(p.bounds.Max.X, r.Max.X+dx))
		r.Max.Y = max(r.Min.Y+1, min(p.bounds.Max.Y, r.Max.Y+dy))
	} else {
		dx = max(-r.Min.X, min(dx, p.bounds.Max.X-r.Max.X))
		dy = max(-r.Min.Y, min(dy, p.bounds.Max.Y-r.Max.Y))
		r = r.Add(image.Pt(dx, dy))
	}
	p.start, p.end = r.Min, r.Max
}
func (p *imagePicker) view(c *ui.Context) {
	if p.done {
		return
	}
	if c.Shortcut(0, ui.KeyEscape) {
		p.finish(image.Rectangle{})
		return
	}
	if c.Shortcut(0, ui.KeyEnter) {
		r := p.selection()
		if !r.Empty() {
			p.finish(r)
			return
		}
	}
	for _, key := range []struct {
		key    ui.Key
		dx, dy int
	}{{ui.KeyLeft, -1, 0}, {ui.KeyRight, 1, 0}, {ui.KeyUp, 0, -1}, {ui.KeyDown, 0, 1}} {
		if c.Shortcut(0, key.key) {
			p.adjust(key.dx, key.dy, false)
		}
		if c.Shortcut(ui.Shift, key.key) {
			p.adjust(key.dx, key.dy, true)
		}
	}
	ui.Column(c).Fill().Padding(12).Gap(10).Children(func() {
		ui.Text(c, p.title).FontWeight(600)
		ui.Text(c, "拖动框选；框内移动，边缘缩放。方向键移动，Shift+方向键调大小，Enter 确认。").FontSize(12).TextColor(c.Theme().TextMuted)
		cursor := p.cursor
		if cursor == ui.CursorDefault {
			cursor = ui.CursorCrosshair
		}
		box := ui.Box(c.Key("image-region")).Label("图片选区").Grow(1).MinHeight(80).FillWidth().Clip().Cursor(cursor)
		box.HandleInput(func(ev ui.InputEvent) bool {
			switch ev.Kind {
			case ui.InputPointerDown:
				if ev.Button != 0 {
					return false
				}
				at := p.point(ev.X, ev.Y)
				p.dragStart, p.original = at, p.selection()
				if h, ok := p.handleAt(ev.X, ev.Y); ok {
					p.mode, p.handle, p.cursor = dragResize, h, handleCursor(h)
				} else if !p.imageRect.Contains(ev.X, ev.Y) {
					return false
				} else if !p.original.Empty() && at.In(p.original) {
					p.mode, p.cursor = dragMove, ui.CursorMove
				} else {
					p.mode, p.cursor = dragDraw, ui.CursorCrosshair
					p.start, p.end = at, at
				}
				p.dragging = true
				return true
			case ui.InputPointerMove:
				if p.dragging {
					p.dragTo(p.point(ev.X, ev.Y))
					return true
				}
				p.cursor = ui.CursorCrosshair
				if h, ok := p.handleAt(ev.X, ev.Y); ok {
					p.cursor = handleCursor(h)
				} else if p.imageRect.Contains(ev.X, ev.Y) && p.point(ev.X, ev.Y).In(p.selection()) {
					p.cursor = ui.CursorMove
				}
				return true
			case ui.InputPointerUp:
				if p.dragging {
					p.dragTo(p.point(ev.X, ev.Y))
					p.dragging = false
					return true
				}
			}
			return false
		}).Draw(func(painter *ui.Painter, r ui.Rect) {
			scale := math.Min(float64(r.W)/float64(p.bounds.Dx()), float64(r.H)/float64(p.bounds.Dy()))
			w, h := float32(float64(p.bounds.Dx())*scale), float32(float64(p.bounds.Dy())*scale)
			p.imageRect = ui.Rect{X: (r.W - w) / 2, Y: (r.H - h) / 2, W: w, H: h}
			at := p.imageRect
			at.X += r.X
			at.Y += r.Y
			painter.Image(p.bitmap, at, ui.FillBox)
			if sel := p.selection(); !sel.Empty() {
				painter.Stroke(ui.Rect{X: at.X + float32(float64(sel.Min.X)*scale), Y: at.Y + float32(float64(sel.Min.Y)*scale), W: float32(float64(sel.Dx()) * scale), H: float32(float64(sel.Dy()) * scale)}, c.Theme().Accent, 0, 2)
				for _, v := range p.handles() {
					handle := ui.Rect{X: r.X + float32(v.X) - 4, Y: r.Y + float32(v.Y) - 4, W: 8, H: 8}
					painter.Fill(handle, ui.RGBA(255, 255, 255, 1), 2)
					painter.Stroke(handle, c.Theme().Accent, 2, 2)
				}
			}
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				p.finish(image.Rectangle{})
			}
			if ui.Button(c, "选择整张").Clicked() {
				p.finish(p.bounds)
			}
			if ui.PrimaryButton(c, "使用选区").Disabled(p.selection().Empty()).Clicked() {
				p.finish(p.selection())
			}
		})
	})
}
