package editor

import (
	"context"
	"errors"
	"fmt"
	"github.com/wildalley/screenpin/internal/brand"
	"image"
	"math"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/doc"
)

// minRegion is the smallest drag, in DIPs, taken as a region rather than a
// stray click.
const minRegion = 4

// pick is ScreenPin's own region picker: the portal hands back one image of
// the whole desktop, each display gets a borderless overlay showing its part
// of it, and the user drags a region there. Releasing confirms, Esc or a
// right click cancels, Enter takes the whole display, and Space held while
// dragging moves the region.
type pick struct {
	full     *image.RGBA
	bitmap   *ui.Bitmap
	views    []*pickView
	done     bool
	result   chan pickResult
	displays []mygo.Display
	minimum  image.Point
	hint     string

	active          *pickView
	origin, corner  doc.Point
	last            doc.Point
	dragging, shift bool
}

type pickView struct {
	display mygo.Display
	crop    image.Rectangle // the display's part of the image, in pixels
	win     *mygo.Window
}

type pickResult struct {
	region image.Rectangle
	target *mygo.Rectangle
	err    error
}

// pickRegion shows the overlays for the PNG of the whole desktop and waits
// for the region. Call it off the main thread.
func PickRegion(ctx context.Context, png []byte, token string) ([]byte, *mygo.Rectangle, *ScreenSelection, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	full, err := capture.Decode(png)
	if err != nil {
		return nil, nil, nil, err
	}
	var displays []mygo.Display
	mygo.RunOnMain(func() { displays = mygo.Screen.Displays() })
	r, target, err := PickScreenRegion(ctx, full, displays, token, image.Pt(minRegion, minRegion), "")
	if err != nil {
		return nil, nil, nil, err
	}
	selection, _ := ScreenSelectionFromDesktop(r, target, displays, full.Bounds())
	data, err := capture.Crop(full, r)
	return data, target, selection, err
}

// PickScreenRegion selects on a display and returns source pixel coordinates.
// A single supplied display maps the stream frame onto that display.
func PickScreenRegion(ctx context.Context, full *image.RGBA, displays []mygo.Display, token string, minimum image.Point, hint string) (image.Rectangle, *mygo.Rectangle, error) {
	if err := ctx.Err(); err != nil {
		return image.Rectangle{}, nil, err
	}
	if full == nil || full.Bounds().Empty() {
		return image.Rectangle{}, nil, errors.New("无法选择空截图")
	}
	var err error
	s := &pick{full: full, displays: displays, minimum: minimum, hint: hint, result: make(chan pickResult, 1)}
	opened := false
	mygo.RunOnMain(func() {
		if ctx.Err() != nil {
			return
		}
		opened = true
		withActivationToken(token, func() { err = s.open() })
	})
	if !opened || err != nil {
		if ctx.Err() != nil {
			return image.Rectangle{}, nil, ctx.Err()
		}
		if err != nil {
			return image.Rectangle{}, nil, err
		}
		return image.Rectangle{}, nil, capture.ErrCancelled
	}
	select {
	case r := <-s.result:
		return r.region, r.target, r.err
	case <-ctx.Done():
		mygo.RunOnMain(func() { s.finish(nil, nil) })
		return image.Rectangle{}, nil, ctx.Err()
	}
}

func (s *pick) open() error {
	displays := s.displays
	if displays == nil {
		displays = mygo.Screen.Displays()
	}
	if len(displays) == 0 {
		return errors.New("没有可用的显示器")
	}
	b := s.full.Bounds()
	crops := layoutScreens(displays, b.Dx(), b.Dy())
	if s.displays != nil && len(displays) == 1 {
		crops[0] = b
	} else if runtime.GOOS == "windows" {
		for i, d := range displays {
			crops[i] = physicalDisplay(d).Intersect(b)
		}
	}
	s.bitmap = ui.NewBitmap(s.full)
	for i, d := range displays {
		v := &pickView{display: d, crop: crops[i]}
		s.views = append(s.views, v)
		v.win = mygo.NewWindow(mygo.WindowOptions{
			Title:           brand.Title("选区"),
			X:               d.Bounds.X,
			Y:               d.Bounds.Y,
			Width:           d.Bounds.Width,
			Height:          d.Bounds.Height,
			UseContentSize:  true,
			Frameless:       true,
			AlwaysOnTop:     true,
			SkipTaskbar:     true,
			DisableResize:   true,
			DisableShadow:   true,
			DisableMinimize: true,
			DisableMaximize: true,
			BackgroundColor: "#000000",
			Content:         ui.View(func(c *ui.Context) { s.view(c, v) }),
		})
		if runtime.GOOS == "windows" {
			capture.ExcludeWindow(v.win.NativeHandle())
		}
		// Window managers place a new window on the display with the
		// pointer, whatever position it asks for, and Wayland gives apps
		// no say at all: full screen on a given display is what both
		// honor.
		if !fullscreenOnDisplay(v.win.NativeHandle(), d.Bounds) {
			v.win.SetBounds(d.Bounds)
			v.win.SetFullScreen(true)
		}
		v.win.OnClose(func(*mygo.CloseEvent) { s.finish(nil, nil) })
		v.win.OnClosed(func() { s.finish(nil, nil) })
	}
	return nil
}

func physicalDisplay(d mygo.Display) image.Rectangle {
	scale := d.ScaleFactor
	if scale <= 0 {
		scale = 1
	}
	b := d.Bounds
	x, y := int(math.Round(float64(b.X)*scale)), int(math.Round(float64(b.Y)*scale))
	return image.Rect(x, y, x+int(math.Round(float64(b.Width)*scale)), y+int(math.Round(float64(b.Height)*scale)))
}

// layoutScreens maps each display onto the image of the whole desktop. The
// portal returns the bounding box of the displays, in pixels; scale it
// uniformly onto their DIPs.
func layoutScreens(displays []mygo.Display, width, height int) []image.Rectangle {
	left, top := math.MaxInt, math.MaxInt
	right, bottom := math.MinInt, math.MinInt
	for _, d := range displays {
		left, top = min(left, d.Bounds.X), min(top, d.Bounds.Y)
		right, bottom = max(right, d.Bounds.X+d.Bounds.Width), max(bottom, d.Bounds.Y+d.Bounds.Height)
	}
	sx := float64(width) / float64(max(1, right-left))
	sy := float64(height) / float64(max(1, bottom-top))
	crops := make([]image.Rectangle, len(displays))
	for i, d := range displays {
		x := int(math.Round(float64(d.Bounds.X-left) * sx))
		y := int(math.Round(float64(d.Bounds.Y-top) * sy))
		w := max(1, min(int(math.Round(float64(d.Bounds.Width)*sx)), width-x))
		h := max(1, min(int(math.Round(float64(d.Bounds.Height)*sy)), height-y))
		crops[i] = image.Rect(x, y, x+w, y+h)
	}
	return crops
}

// DesktopRegionToStream converts a whole-desktop selection into coordinates
// of the selected display's stream. Display geometry, rather than enumeration
// order or monitor name, identifies the selected screen.
func DesktopRegionToStream(region image.Rectangle, target mygo.Rectangle, displays []mygo.Display, desktopSize, sourceSize image.Point) (image.Rectangle, error) {
	if desktopSize.X <= 0 || desktopSize.Y <= 0 || sourceSize.X <= 0 || sourceSize.Y <= 0 || region.Empty() {
		return image.Rectangle{}, errors.New("无效的桌面选区或采集尺寸")
	}
	crops := layoutScreens(displays, desktopSize.X, desktopSize.Y)
	for i, display := range displays {
		if display.Bounds != target {
			continue
		}
		crop := crops[i]
		if !region.In(crop) {
			return image.Rectangle{}, errors.New("长截图选区超出了所选显示器")
		}
		sx, sy := float64(sourceSize.X)/float64(crop.Dx()), float64(sourceSize.Y)/float64(crop.Dy())
		return image.Rect(
			int(math.Floor(float64(region.Min.X-crop.Min.X)*sx)),
			int(math.Floor(float64(region.Min.Y-crop.Min.Y)*sy)),
			int(math.Ceil(float64(region.Max.X-crop.Min.X)*sx)),
			int(math.Ceil(float64(region.Max.Y-crop.Min.Y)*sy)),
		).Intersect(image.Rectangle{Max: sourceSize}), nil
	}
	return image.Rectangle{}, errors.New("框选的显示器已断开，请重新截图")
}

// regionToCrop maps a region of an overlay, in its DIPs, onto the image,
// inside the overlay's display.
func regionToCrop(r doc.Rect, viewW, viewH float64, crop image.Rectangle) (image.Rectangle, error) {
	if viewW <= 0 || viewH <= 0 {
		return image.Rectangle{}, errors.New("选区窗口尺寸无效")
	}
	cw, ch := float64(crop.Dx()), float64(crop.Dy())
	sx, sy := cw/viewW, ch/viewH
	left := clamp(math.Floor(r.X*sx), 0, cw-1)
	top := clamp(math.Floor(r.Y*sy), 0, ch-1)
	right := clamp(math.Ceil((r.X+r.W)*sx), left+1, cw)
	bottom := clamp(math.Ceil((r.Y+r.H)*sy), top+1, ch)
	return image.Rect(crop.Min.X+int(left), crop.Min.Y+int(top), crop.Min.X+int(right), crop.Min.Y+int(bottom)), nil
}

// finish ends the picking once: region nil cancels.
func (s *pick) finish(v *pickView, region *doc.Rect) {
	if s.done {
		return
	}
	if region != nil {
		r, err := regionToCrop(*region, float64(v.display.Bounds.Width), float64(v.display.Bounds.Height), v.crop)
		if err != nil || r.Dx() < s.minimum.X || r.Dy() < s.minimum.Y {
			return
		}
	}
	s.done = true
	for _, o := range s.views {
		o.win.Destroy()
	}
	s.full, s.bitmap, s.views, s.active, s.displays = nil, nil, nil, nil, nil
	s.dragging, s.shift = false, false
	if region == nil {
		s.result <- pickResult{err: capture.ErrCancelled}
		return
	}
	w, h := float64(v.display.Bounds.Width), float64(v.display.Bounds.Height)
	target := v.display.Bounds
	r, err := regionToCrop(*region, w, h, v.crop)
	s.result <- pickResult{region: r, target: &target, err: err}
}

func (s *pick) rect() doc.Rect {
	return doc.Rect{
		X: math.Min(s.origin.X, s.corner.X), Y: math.Min(s.origin.Y, s.corner.Y),
		W: math.Abs(s.corner.X - s.origin.X), H: math.Abs(s.corner.Y - s.origin.Y),
	}
}

func (s *pick) input(v *pickView, ev ui.InputEvent, w, h float64) bool {
	switch ev.Kind {
	case ui.InputKeyDown:
		switch ev.Key {
		case ui.KeyEscape:
			s.finish(nil, nil)
		case ui.KeyEnter:
			s.finish(v, &doc.Rect{W: w, H: h})
		case ui.KeySpace:
			s.shift = true
		default:
			return false
		}
		return true
	case ui.InputKeyUp:
		if ev.Key == ui.KeySpace {
			s.shift = false
			return true
		}
	case ui.InputPointerDown:
		if ev.Button == 1 {
			s.finish(nil, nil)
			return true
		}
		if ev.Button != 0 {
			return false
		}
		p := doc.Point{X: clamp(float64(ev.X), 0, w), Y: clamp(float64(ev.Y), 0, h)}
		s.active, s.dragging = v, true
		s.origin, s.corner, s.last = p, p, p
		v.win.Focus()
		return true
	case ui.InputPointerMove:
		if !s.dragging || s.active != v {
			return false
		}
		p := doc.Point{X: clamp(float64(ev.X), 0, w), Y: clamp(float64(ev.Y), 0, h)}
		if s.shift {
			// Move both corners, stopping at the edges so the region keeps
			// its size.
			dx := clamp(p.X-s.last.X, -math.Min(s.origin.X, s.corner.X), w-math.Max(s.origin.X, s.corner.X))
			dy := clamp(p.Y-s.last.Y, -math.Min(s.origin.Y, s.corner.Y), h-math.Max(s.origin.Y, s.corner.Y))
			s.origin.X, s.origin.Y = s.origin.X+dx, s.origin.Y+dy
			s.corner.X, s.corner.Y = s.corner.X+dx, s.corner.Y+dy
		} else {
			s.corner = p
		}
		s.last = p
		return true
	case ui.InputPointerUp:
		if !s.dragging || s.active != v {
			return false
		}
		if ev.Button != 0 {
			return false
		}
		if ev.Canceled {
			s.dragging, s.shift, s.active = false, false, nil
			s.origin, s.corner, s.last = doc.Point{}, doc.Point{}, doc.Point{}
			return true
		}
		if !s.shift {
			s.corner = doc.Point{X: clamp(float64(ev.X), 0, w), Y: clamp(float64(ev.Y), 0, h)}
		}
		s.dragging = false
		if r := s.rect(); r.W >= minRegion && r.H >= minRegion {
			px, err := regionToCrop(r, w, h, v.crop)
			if err == nil && px.Dx() >= s.minimum.X && px.Dy() >= s.minimum.Y {
				s.finish(v, &r)
			}
		} else {
			s.active = nil
		}
		return true
	}
	return false
}

var (
	pickDim    = ui.RGBA(0, 0, 0, 0.38)
	pickWhite  = ui.RGBA(255, 255, 255, 1)
	pickHintBg = ui.RGBA(20, 24, 25, 0.82)
)

func (s *pick) view(c *ui.Context, v *pickView) {
	brand.Apply(c)
	if s.done || s.bitmap == nil {
		return
	}
	w, h := c.Size()
	box := ui.Box(c.Key("pick")).Fill().Cursor(ui.CursorCrosshair).Focusable().FocusRing(false).Focus()
	box.HandleInput(func(ev ui.InputEvent) bool { return s.input(v, ev, float64(w), float64(h)) })
	bm, crop, bmOrigin := s.bitmap, v.crop, s.full.Bounds().Min
	var sel *doc.Rect
	if s.active == v {
		r := s.rect()
		sel = &r
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		p.ImageRegion(bm, ui.Rect{X: float32(crop.Min.X - bmOrigin.X), Y: float32(crop.Min.Y - bmOrigin.Y), W: float32(crop.Dx()), H: float32(crop.Dy())}, r, ui.ImageOptions{})
		if sel == nil || sel.W <= 0 || sel.H <= 0 {
			p.Fill(r, pickDim, 0)
			text := s.hint
			if text == "" {
				text = "拖动选择区域，松开截取 · 拖动时按住空格移动选区 · Enter 截取整屏 · Esc 取消"
			}
			hint := ui.Span{Text: text, Size: 13, Color: pickWhite}
			tw, th := p.MeasureText(0, hint)
			pill := ui.Rect{X: r.X + (r.W-tw)/2 - 14, Y: r.Y + 48, W: tw + 28, H: th + 14}
			p.Fill(pill, pickHintBg, 6)
			p.RichText(pill.X+14, pill.Y+7, 0, hint)
			return
		}
		x, y := r.X+float32(sel.X), r.Y+float32(sel.Y)
		sw, sh := float32(sel.W), float32(sel.H)
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: y - r.Y}, pickDim, 0)
		p.Fill(ui.Rect{X: r.X, Y: y + sh, W: r.W, H: r.Y + r.H - y - sh}, pickDim, 0)
		p.Fill(ui.Rect{X: r.X, Y: y, W: x - r.X, H: sh}, pickDim, 0)
		p.Fill(ui.Rect{X: x + sw, Y: y, W: r.X + r.W - x - sw, H: sh}, pickDim, 0)
		p.Stroke(ui.Rect{X: x, Y: y, W: sw, H: sh}, p.Theme().Accent, 0, 1)
		px, err := regionToCrop(*sel, float64(r.W), float64(r.H), crop)
		if err != nil {
			return
		}
		text := fmt.Sprintf("%d × %d", px.Dx(), px.Dy())
		if px.Dx() < s.minimum.X || px.Dy() < s.minimum.Y {
			text += fmt.Sprintf(" · 至少 %d × %d", s.minimum.X, s.minimum.Y)
		}
		label := ui.Span{Text: text, Size: 12, Color: pickWhite}
		lw, lh := p.MeasureText(0, label)
		at := ui.Rect{X: x, Y: y - lh - 10, W: lw + 12, H: lh + 4}
		if y-r.Y < 28 {
			at.X, at.Y = x+6, y+6
		}
		p.Fill(at, p.Theme().AccentPressed, 4)
		p.RichText(at.X+6, at.Y+2, 0, label)
	})
	_ = h
}
