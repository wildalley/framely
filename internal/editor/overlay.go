package editor

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/brand"
	"github.com/wildalley/framely/internal/capture"
	"github.com/wildalley/framely/internal/doc"
)

// minRegion is the smallest drag, in DIPs, taken as a region rather than a
// stray click.
const minRegion = 4

// PickAction is what the user chose to do with a picked region.
type PickAction int

const (
	PickEdit   PickAction = iota // open the image window
	PickCopy                     // copy the region, no window
	PickPin                      // open it pinned
	PickSave                     // open it and ask where to save
	PickScroll                   // start a long capture of the region
	PickRecord                   // record the region
	PickText                     // recognize the region's text
)

// PickOptions sets what the picker offers. With Actions, a released region
// stays adjustable and a bar beside it chooses what to do; without, the
// release confirms at once, as long capture and recording want. Scroll,
// Record and Text add those actions to the bar.
type PickOptions struct {
	Minimum              image.Point
	Hint                 string
	Actions              bool
	Scroll, Record, Text bool
}

// Pick is a confirmed region: source pixels, the display it was on, the
// chosen action and, for PickEdit, the annotation tool to start with.
type Pick struct {
	Region image.Rectangle
	Target *mygo.Rectangle
	Action PickAction
	Tool   doc.Tool
}

// pick is Framely's own region picker: each display gets a borderless
// overlay showing its part of one image of the whole desktop, and the user
// drags a region there. Esc cancels; a right click clears the region, or
// cancels when there is none; Enter takes the whole display; Space held
// while dragging moves the region. A magnifier follows the pointer and C
// copies the color under it.
type pick struct {
	full     *image.RGBA
	bitmap   *ui.Bitmap
	views    []*pickView
	done     bool
	result   chan pickResult
	displays []mygo.Display
	opts     PickOptions

	active         *pickView
	origin, corner doc.Point
	last           doc.Point
	dragging       bool
	space          bool // Space held: the drag moves the region
	// adjusting: a released region on active that can still move and
	// resize, held in sel, until an action confirms it.
	adjusting bool
	sel       doc.Rect
	drag      pickDrag
	handle    int
	pointer   doc.Point
	hover     *pickView
}

type pickDrag int

const (
	dragCreate pickDrag = iota
	dragMoveRegion
	dragResizeRegion
)

type pickView struct {
	display mygo.Display
	crop    image.Rectangle // the display's part of the image, in pixels
	win     *mygo.Window
}

type pickResult struct {
	Pick
	err error
}

// lastPick is the region confirmed last, which R brings back on the same
// display.
var lastPick struct {
	display mygo.Rectangle
	rect    doc.Rect
	ok      bool
}

// PickRegion shows the overlays for the PNG of the whole desktop and waits
// for the region and what to do with it. Call it off the main thread.
func PickRegion(ctx context.Context, png []byte, token string, opts PickOptions) ([]byte, Pick, *ScreenSelection, error) {
	if err := ctx.Err(); err != nil {
		return nil, Pick{}, nil, err
	}
	full, err := capture.Decode(png)
	if err != nil {
		return nil, Pick{}, nil, err
	}
	var displays []mygo.Display
	mygo.RunOnMain(func() { displays = mygo.Screen.Displays() })
	if opts.Minimum == (image.Point{}) {
		opts.Minimum = image.Pt(minRegion, minRegion)
	}
	choice, err := PickScreen(ctx, full, displays, token, opts)
	if err != nil {
		return nil, Pick{}, nil, err
	}
	selection, _ := ScreenSelectionFromDesktop(choice.Region, choice.Target, displays, full.Bounds())
	data, err := capture.Crop(full, choice.Region)
	return data, choice, selection, err
}

// PickScreenRegion selects on a display and returns source pixel
// coordinates as soon as the pointer is released. A single supplied
// display maps the stream frame onto that display.
func PickScreenRegion(ctx context.Context, full *image.RGBA, displays []mygo.Display, token string, minimum image.Point, hint string) (image.Rectangle, *mygo.Rectangle, error) {
	choice, err := PickScreen(ctx, full, displays, token, PickOptions{Minimum: minimum, Hint: hint})
	return choice.Region, choice.Target, err
}

// PickScreen selects on a display with opts and returns the choice.
func PickScreen(ctx context.Context, full *image.RGBA, displays []mygo.Display, token string, opts PickOptions) (Pick, error) {
	if err := ctx.Err(); err != nil {
		return Pick{}, err
	}
	if full == nil || full.Bounds().Empty() {
		return Pick{}, errors.New("无法选择空截图")
	}
	var err error
	s := &pick{full: full, displays: displays, opts: opts, result: make(chan pickResult, 1)}
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
			return Pick{}, ctx.Err()
		}
		if err != nil {
			return Pick{}, err
		}
		return Pick{}, capture.ErrCancelled
	}
	select {
	case r := <-s.result:
		return r.Pick, r.err
	case <-ctx.Done():
		mygo.RunOnMain(func() { s.cancel() })
		return Pick{}, ctx.Err()
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
	s.bitmap = ui.NewBitmap(s.full).Pixelated()
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
		v.win.OnClose(func(*mygo.CloseEvent) { s.cancel() })
		v.win.OnClosed(func() { s.cancel() })
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

// cancel ends the picking without a region.
func (s *pick) cancel() { s.finish(nil, nil, Pick{}) }

// finish ends the picking once: region nil cancels. A region under the
// minimum leaves the picker open.
func (s *pick) finish(v *pickView, region *doc.Rect, choice Pick) {
	if s.done {
		return
	}
	var r image.Rectangle
	if region != nil {
		var err error
		r, err = regionToCrop(*region, float64(v.display.Bounds.Width), float64(v.display.Bounds.Height), v.crop)
		if err != nil || r.Dx() < s.opts.Minimum.X || r.Dy() < s.opts.Minimum.Y {
			return
		}
	}
	s.done = true
	for _, o := range s.views {
		o.win.Destroy()
	}
	s.full, s.bitmap, s.views, s.active, s.hover, s.displays = nil, nil, nil, nil, nil, nil
	s.dragging, s.space, s.adjusting = false, false, false
	if region == nil {
		s.result <- pickResult{err: capture.ErrCancelled}
		return
	}
	lastPick.display, lastPick.rect, lastPick.ok = v.display.Bounds, *region, true
	target := v.display.Bounds
	choice.Region, choice.Target = r, &target
	s.result <- pickResult{Pick: choice}
}

func (s *pick) rect() doc.Rect {
	return doc.Rect{
		X: math.Min(s.origin.X, s.corner.X), Y: math.Min(s.origin.Y, s.corner.Y),
		W: math.Abs(s.corner.X - s.origin.X), H: math.Abs(s.corner.Y - s.origin.Y),
	}
}

// shown is the region drawn on v: the one being dragged out, or the one
// being adjusted.
func (s *pick) shown(v *pickView) (doc.Rect, bool) {
	if s.active != v {
		return doc.Rect{}, false
	}
	if s.dragging && s.drag == dragCreate {
		return s.rect(), true
	}
	return s.sel, s.adjusting
}

const pickHandle = 6 // half the side of a resize handle, in DIPs

// pickHandles are the corners then the edges' middles: top-left, top-right,
// bottom-right, bottom-left, top, right, bottom, left.
func pickHandles(r doc.Rect) [8]doc.Point {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	return [8]doc.Point{
		{X: r.X, Y: r.Y}, {X: r.X + r.W, Y: r.Y}, {X: r.X + r.W, Y: r.Y + r.H}, {X: r.X, Y: r.Y + r.H},
		{X: cx, Y: r.Y}, {X: r.X + r.W, Y: cy}, {X: cx, Y: r.Y + r.H}, {X: r.X, Y: cy},
	}
}

func pickHandleAt(r doc.Rect, p doc.Point) int {
	for i, h := range pickHandles(r) {
		if math.Abs(p.X-h.X) <= pickHandle+3 && math.Abs(p.Y-h.Y) <= pickHandle+3 {
			return i
		}
	}
	return -1
}

func pickHandleCursor(i int) ui.Cursor {
	switch i {
	case 0, 2:
		return ui.CursorResizeNWSE
	case 1, 3:
		return ui.CursorResizeNESW
	case 4, 6:
		return ui.CursorResizeNS
	default:
		return ui.CursorResizeEW
	}
}

// resize moves the edges handle i holds to p, keeping the region inside
// w×h and the right way round.
func resizeRegion(r doc.Rect, i int, p doc.Point) doc.Rect {
	left, top, right, bottom := r.X, r.Y, r.X+r.W, r.Y+r.H
	switch i {
	case 0:
		left, top = p.X, p.Y
	case 1:
		right, top = p.X, p.Y
	case 2:
		right, bottom = p.X, p.Y
	case 3:
		left, bottom = p.X, p.Y
	case 4:
		top = p.Y
	case 5:
		right = p.X
	case 6:
		bottom = p.Y
	case 7:
		left = p.X
	}
	return doc.Rect{X: math.Min(left, right), Y: math.Min(top, bottom), W: math.Abs(right - left), H: math.Abs(bottom - top)}
}

func moveRegion(r doc.Rect, dx, dy, w, h float64) doc.Rect {
	r.X = clamp(r.X+dx, 0, w-r.W)
	r.Y = clamp(r.Y+dy, 0, h-r.H)
	return r
}

func (s *pick) startAdjusting(v *pickView, r doc.Rect) {
	s.active, s.sel, s.adjusting, s.dragging = v, r, true, false
}

// confirm ends the picking with the region v shows, or with the whole
// display when nothing is drawn.
func (s *pick) confirm(v *pickView, choice Pick, w, h float64) {
	r, ok := s.shown(v)
	if !ok {
		r = doc.Rect{W: w, H: h}
	}
	s.finish(v, &r, choice)
}

func (s *pick) input(v *pickView, ev ui.InputEvent, w, h float64) bool {
	switch ev.Kind {
	case ui.InputKeyDown:
		return s.key(v, ev, w, h)
	case ui.InputKeyUp:
		if ev.Key == ui.KeySpace {
			s.space = false
			return true
		}
	case ui.InputPointerDown:
		p := doc.Point{X: clamp(float64(ev.X), 0, w), Y: clamp(float64(ev.Y), 0, h)}
		s.pointer, s.hover = p, v
		if ev.Button == 1 {
			if s.adjusting {
				s.adjusting, s.active = false, nil // Draw it again.
			} else {
				s.cancel()
			}
			return true
		}
		if ev.Button != 0 {
			return false
		}
		if v.win != nil {
			v.win.Focus()
		}
		if s.adjusting && s.active == v {
			if ev.Clicks >= 2 && s.sel.Contains(p) {
				s.finish(v, &s.sel, Pick{Action: PickPin})
				return true
			}
			if i := pickHandleAt(s.sel, p); i >= 0 {
				s.dragging, s.drag, s.handle, s.last = true, dragResizeRegion, i, p
				return true
			}
			if s.sel.Contains(p) {
				s.dragging, s.drag, s.last = true, dragMoveRegion, p
				return true
			}
		}
		s.active, s.dragging, s.drag, s.adjusting = v, true, dragCreate, false
		s.origin, s.corner, s.last = p, p, p
		return true
	case ui.InputPointerMove:
		p := doc.Point{X: clamp(float64(ev.X), 0, w), Y: clamp(float64(ev.Y), 0, h)}
		s.pointer, s.hover = p, v
		if !s.dragging || s.active != v {
			return true // Redraw the magnifier.
		}
		switch s.drag {
		case dragCreate:
			if s.space {
				// Move both corners, stopping at the edges so the region
				// keeps its size.
				dx := clamp(p.X-s.last.X, -math.Min(s.origin.X, s.corner.X), w-math.Max(s.origin.X, s.corner.X))
				dy := clamp(p.Y-s.last.Y, -math.Min(s.origin.Y, s.corner.Y), h-math.Max(s.origin.Y, s.corner.Y))
				s.origin.X, s.origin.Y = s.origin.X+dx, s.origin.Y+dy
				s.corner.X, s.corner.Y = s.corner.X+dx, s.corner.Y+dy
			} else {
				s.corner = p
			}
		case dragMoveRegion:
			s.sel = moveRegion(s.sel, p.X-s.last.X, p.Y-s.last.Y, w, h)
		case dragResizeRegion:
			s.sel = resizeRegion(s.sel, s.handle, p)
		}
		s.last = p
		return true
	case ui.InputPointerUp:
		if !s.dragging || s.active != v || ev.Button != 0 {
			return false
		}
		if ev.Canceled {
			s.dragging, s.space = false, false
			if s.drag == dragCreate {
				s.active = nil
			}
			return true
		}
		s.dragging = false
		if s.drag != dragCreate {
			if s.sel.W < minRegion || s.sel.H < minRegion {
				s.adjusting, s.active = false, nil
			}
			return true
		}
		if !s.space {
			s.corner = doc.Point{X: clamp(float64(ev.X), 0, w), Y: clamp(float64(ev.Y), 0, h)}
		}
		r := s.rect()
		if r.W < minRegion || r.H < minRegion {
			s.active = nil
			return true
		}
		if s.opts.Actions {
			s.startAdjusting(v, r)
			return true
		}
		s.finish(v, &r, Pick{})
		return true
	}
	return false
}

func (s *pick) key(v *pickView, ev ui.InputEvent, w, h float64) bool {
	switch ev.Key {
	case ui.KeyEscape:
		s.cancel()
	case ui.KeyEnter:
		if !s.opts.Actions {
			s.confirm(v, Pick{}, w, h)
		} else if r, ok := s.shown(v); ok {
			s.finish(v, &r, Pick{Action: PickCopy})
		} else {
			s.startAdjusting(v, doc.Rect{W: w, H: h})
		}
	case ui.KeySpace:
		s.space = true
	case ui.KeyR:
		if !lastPick.ok || lastPick.display != v.display.Bounds || s.dragging {
			return false
		}
		r := lastPick.rect
		if !s.opts.Actions {
			s.finish(v, &r, Pick{})
		} else {
			s.startAdjusting(v, r)
		}
	case ui.KeyC:
		if s.hover != v {
			return false
		}
		if hex, ok := s.colorAt(v, s.pointer, w, h); ok {
			mygo.Clipboard.WriteText(hex)
			s.cancel()
		}
	case ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown:
		if !s.adjusting || s.active != v || s.dragging {
			return false
		}
		step := 1.0
		if ev.Mods&ui.Shift != 0 {
			step = 10
		}
		dx, dy := 0.0, 0.0
		switch ev.Key {
		case ui.KeyLeft:
			dx = -step
		case ui.KeyRight:
			dx = step
		case ui.KeyUp:
			dy = -step
		case ui.KeyDown:
			dy = step
		}
		s.sel = moveRegion(s.sel, dx, dy, w, h)
	default:
		return false
	}
	return true
}

// pixelAt is the source pixel under p on v.
func (s *pick) pixelAt(v *pickView, p doc.Point, w, h float64) (image.Point, bool) {
	if s.full == nil || w <= 0 || h <= 0 {
		return image.Point{}, false
	}
	x := v.crop.Min.X + int(math.Floor(p.X*float64(v.crop.Dx())/w))
	y := v.crop.Min.Y + int(math.Floor(p.Y*float64(v.crop.Dy())/h))
	pt := image.Pt(min(x, v.crop.Max.X-1), min(y, v.crop.Max.Y-1))
	return pt, pt.In(s.full.Bounds())
}

func (s *pick) colorAt(v *pickView, p doc.Point, w, h float64) (string, bool) {
	pt, ok := s.pixelAt(v, p, w, h)
	if !ok {
		return "", false
	}
	c := s.full.RGBAAt(pt.X, pt.Y)
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B), true
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
	sel, shown := s.shown(v)
	// The action bar is a sibling over the region's canvas, so that its
	// clicks reach its buttons rather than start a new region.
	ui.Box(c.Key("pick-root")).Fill().Children(func() { s.canvas(c, v, sel, shown, w, h) })
}

func (s *pick) canvas(c *ui.Context, v *pickView, sel doc.Rect, shown bool, w, h float32) {
	box := ui.Box(c.Key("pick")).Label("选区").Fill().Cursor(ui.CursorCrosshair).Focusable().FocusRing(false).Focus()
	if s.adjusting && s.active == v && s.hover == v {
		if s.dragging && s.drag == dragResizeRegion {
			box.Cursor(pickHandleCursor(s.handle))
		} else if i := pickHandleAt(sel, s.pointer); i >= 0 {
			box.Cursor(pickHandleCursor(i))
		} else if sel.Contains(s.pointer) {
			box.Cursor(ui.CursorMove)
		}
	}
	box.HandleInput(func(ev ui.InputEvent) bool { return s.input(v, ev, float64(w), float64(h)) })
	bm, crop, bmOrigin := s.bitmap, v.crop, s.full.Bounds().Min
	magnify := s.hover == v && (!s.adjusting || s.dragging && s.drag == dragResizeRegion)
	pointer := s.pointer
	hex, _ := s.colorAt(v, pointer, float64(w), float64(h))
	pixel, _ := s.pixelAt(v, pointer, float64(w), float64(h))
	adjusting := s.adjusting && s.active == v
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		p.ImageRegion(bm, ui.Rect{X: float32(crop.Min.X - bmOrigin.X), Y: float32(crop.Min.Y - bmOrigin.Y), W: float32(crop.Dx()), H: float32(crop.Dy())}, r, ui.ImageOptions{})
		if !shown || sel.W <= 0 || sel.H <= 0 {
			p.Fill(r, pickDim, 0)
			text := s.opts.Hint
			if text == "" {
				text = "拖动选择区域 · Enter 整屏 · R 上次区域 · C 复制颜色 · Esc 取消"
			}
			hint := ui.Span{Text: text, Size: 13, Color: pickWhite}
			tw, th := p.MeasureText(0, hint)
			pill := ui.Rect{X: r.X + (r.W-tw)/2 - 14, Y: r.Y + 48, W: tw + 28, H: th + 14}
			p.Fill(pill, pickHintBg, 6)
			p.RichText(pill.X+14, pill.Y+7, 0, hint)
		} else {
			x, y := r.X+float32(sel.X), r.Y+float32(sel.Y)
			sw, sh := float32(sel.W), float32(sel.H)
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: y - r.Y}, pickDim, 0)
			p.Fill(ui.Rect{X: r.X, Y: y + sh, W: r.W, H: r.Y + r.H - y - sh}, pickDim, 0)
			p.Fill(ui.Rect{X: r.X, Y: y, W: x - r.X, H: sh}, pickDim, 0)
			p.Fill(ui.Rect{X: x + sw, Y: y, W: r.X + r.W - x - sw, H: sh}, pickDim, 0)
			p.Stroke(ui.Rect{X: x, Y: y, W: sw, H: sh}, brand.Signal, 0, 1.5)
			if adjusting {
				for _, hp := range pickHandles(sel) {
					hr := ui.Rect{X: r.X + float32(hp.X) - pickHandle, Y: r.Y + float32(hp.Y) - pickHandle, W: 2 * pickHandle, H: 2 * pickHandle}
					p.Fill(hr, pickWhite, 2)
					p.Stroke(hr, brand.Signal, 2, 1.5)
				}
			}
			if px, err := regionToCrop(sel, float64(r.W), float64(r.H), crop); err == nil {
				text := fmt.Sprintf("%d × %d", px.Dx(), px.Dy())
				if px.Dx() < s.opts.Minimum.X || px.Dy() < s.opts.Minimum.Y {
					text += fmt.Sprintf(" · 至少 %d × %d", s.opts.Minimum.X, s.opts.Minimum.Y)
				}
				label := ui.Span{Text: text, Size: 12, Color: pickWhite}
				lw, lh := p.MeasureText(0, label)
				at := ui.Rect{X: x, Y: y - lh - 10, W: lw + 12, H: lh + 4}
				if y-r.Y < 28 {
					at.X, at.Y = x+6, y+6
				}
				p.Fill(at, pickHintBg, 4)
				p.RichText(at.X+6, at.Y+2, 0, label)
			}
		}
		if magnify && hex != "" {
			drawMagnifier(p, r, bm, bmOrigin, pixel, pointer, hex)
		}
	})
	if adjusting && !s.dragging && s.opts.Actions {
		s.actionBar(c, v, sel, w, h)
	}
}

const (
	loupeCells = 15 // source pixels across the magnifier
	loupeSize  = 120
)

// drawMagnifier shows the pixels around the pointer, the one under it
// outlined, with its color and position, beside the pointer and away from
// the screen's edges.
func drawMagnifier(p *ui.Painter, r ui.Rect, bm *ui.Bitmap, origin, pixel image.Point, pointer doc.Point, hex string) {
	info := []ui.Span{{Text: hex, Size: 12, Color: pickWhite, Font: "monospace"}}
	coords := ui.Span{Text: fmt.Sprintf("%d, %d · C 复制", pixel.X, pixel.Y), Size: 11, Color: pickWhite.Alpha(.75)}
	panel := ui.Rect{X: r.X + float32(pointer.X) + 20, Y: r.Y + float32(pointer.Y) + 20, W: loupeSize + 12, H: loupeSize + 52}
	if panel.X+panel.W > r.X+r.W {
		panel.X = r.X + float32(pointer.X) - 20 - panel.W
	}
	if panel.Y+panel.H > r.Y+r.H {
		panel.Y = r.Y + float32(pointer.Y) - 20 - panel.H
	}
	p.Fill(panel, pickHintBg, 10)
	view := ui.Rect{X: panel.X + 6, Y: panel.Y + 6, W: loupeSize, H: loupeSize}
	half := loupeCells / 2
	src := ui.Rect{X: float32(pixel.X - origin.X - half), Y: float32(pixel.Y - origin.Y - half), W: loupeCells, H: loupeCells}
	p.Clip(view, 6, func() {
		p.Fill(view, ui.RGBA(0, 0, 0, 1), 0)
		p.ImageRegion(bm, src, view, ui.ImageOptions{})
	})
	cell := float32(loupeSize) / loupeCells
	center := ui.Rect{X: view.X + cell*float32(half), Y: view.Y + cell*float32(half), W: cell, H: cell}
	p.Stroke(center, pickWhite, 0, 1.5)
	p.Stroke(ui.Rect{X: center.X - 1, Y: center.Y - 1, W: cell + 2, H: cell + 2}, ui.RGBA(0, 0, 0, .6), 0, 1)
	sw := ui.Rect{X: panel.X + 8, Y: view.Y + view.H + 9, W: 14, H: 14}
	p.Fill(sw, ui.Hex(hex), 3)
	p.Stroke(sw, pickWhite.Alpha(.6), 3, 1)
	p.RichText(sw.X+20, sw.Y-1, 0, info...)
	p.RichText(panel.X+8, sw.Y+18, 0, coords)
}

// actionBar sits under the region, or above or inside it where the screen
// ends, and confirms the region with what to do.
func (s *pick) actionBar(c *ui.Context, v *pickView, sel doc.Rect, w, h float32) {
	const barHeight = 44
	t := c.Theme()
	bar := ui.Row(c.Key("pick-actions")).Role(ui.RoleToolbar).FocusGroup(ui.Horizontal).Label("选区操作").
		Absolute().Height(barHeight).Padding(4).Gap(2).AlignItems(ui.Center).Radius(12).
		Background(t.Background).Border(1, t.Border).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, .3))
	top := float32(sel.Y+sel.H) + 8
	if top+barHeight > h-8 {
		top = float32(sel.Y) - 8 - barHeight
		if top < 8 {
			top = float32(sel.Y+sel.H) - 8 - barHeight
		}
	}
	bar.Top(max(8, top))
	if right := w - float32(sel.X+sel.W); float32(sel.X+sel.W) > 560 {
		bar.Right(max(8, right))
	} else {
		bar.Left(max(8, float32(sel.X)))
	}
	choose := func(choice Pick) { s.finish(v, &s.sel, choice) }
	bar.Children(func() {
		for _, it := range []toolItem{toolItems[1], toolItems[3], toolItems[4], toolItems[7], toolItems[8]} {
			if iconButton(c, "pick-tool-"+string(it.tool), it.icon, it.label, false).Clicked() {
				choose(Pick{Action: PickEdit, Tool: it.tool})
				return
			}
		}
		toolbarDivider(c, "pick-div-tools")
		text := func(key, label string, action PickAction) bool {
			if ui.Button(c.Key(key), label).Height(34).Padding(0, 9).Radius(8).FontSize(13).Shrink(0).
				Background(ui.Transparent).Border(0, ui.Transparent).Label(label).Clicked() {
				choose(Pick{Action: action})
				return true
			}
			return false
		}
		if s.opts.Scroll && text("pick-scroll", "长截图", PickScroll) {
			return
		}
		if s.opts.Record && text("pick-record", "录屏", PickRecord) {
			return
		}
		if s.opts.Text && text("pick-text", "取字", PickText) {
			return
		}
		toolbarDivider(c, "pick-div-actions")
		if iconButton(c, "pick-pin", iconPin, "钉在屏幕上（双击选区）", false).Clicked() {
			choose(Pick{Action: PickPin})
			return
		}
		if iconButton(c, "pick-save", iconDownload, "保存", false).Clicked() {
			choose(Pick{Action: PickSave})
			return
		}
		if iconButton(c, "pick-edit", iconFit, "打开编辑窗口", false).Clicked() {
			choose(Pick{Action: PickEdit})
			return
		}
		if iconButton(c, "pick-cancel", iconClose, "取消 (Esc)", false).Clicked() {
			s.cancel()
			return
		}
		if ui.PrimaryButton(c.Key("pick-copy"), "复制").Height(34).Padding(0, 14).Radius(8).Shrink(0).
			Tooltip("复制到剪贴板 (Enter)").Clicked() {
			choose(Pick{Action: PickCopy})
		}
	})
}
