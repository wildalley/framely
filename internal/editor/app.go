// Package editor owns temporary image windows and the screenshot region picker.
package editor

import (
	"context"
	"github.com/wildalley/framely/internal/brand"
	"image"
	"math"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/doc"
	"github.com/wildalley/framely/internal/imagelimit"
	"github.com/wildalley/framely/internal/ocr"
	"github.com/wildalley/framely/internal/render"
)

type Panel int

const (
	PanelNone Panel = iota
	PanelStyle
)

const (
	minZoom = 0.1
	maxZoom = 3
	// The window never gets smaller than this, so the top bar and the tool
	// rail keep their room; a smaller image sits centered on the stage.
	minWidth    = 480
	minHeight   = 360
	styleWidth  = 280
	styleHeight = 330
)

// App belongs to one image. Closing it cancels its work and drops the image,
// annotations and undo history; only the desktop coordinator stays in the tray.
// Mutable fields are accessed on the UI thread. ctx and win do not change once
// Open returns, so workers may safely use them to dispatch guarded results.
type App struct {
	win    *mygo.Window
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
	cfg    config.Config

	image    *doc.Image
	pixels   *image.RGBA
	mosaicPx *image.RGBA
	bitmap   *ui.Bitmap
	mosaic   *ui.Bitmap
	title    string
	history  doc.History
	lease    *imagelimit.Lease

	tool          doc.Tool
	selected      string
	styles        map[doc.Tool]doc.Style
	zoom          float64
	pinned        bool
	busy          bool
	ocrRunning    bool
	ocrCancel     context.CancelFunc
	panel         Panel
	lastPanel     Panel
	appearanceTab int
	// styleHidden puts the style flyout away until the tool or the
	// selection changes.
	styleHidden bool
	toasts      []string

	exportFormat  render.Format
	exportQuality float64

	drag           *drag
	hoverCursor    ui.Cursor
	editingID      string
	editingText    string
	editFresh      bool
	annotationText ui.Handle
	styleText      ui.Handle
	slidingKey     string
	textEdit       bool
	displayZoom    float64

	target    *mygo.Rectangle
	selection *ScreenSelection
	layout    layoutKey

	OnClosed    func()
	OnPaste     func()
	OnCapture   func()
	OnScroll    func()
	OnRecord    func()
	OnGIF       func()
	OnSettings  func()
	OnOCRResult func(ocr.Result)
	OnOCRStart  func(context.CancelFunc) (func(ocr.Progress), func(ocr.Result, error))
}

func New(cfg config.Config) *App {
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{
		ctx: ctx, cancel: cancel, cfg: cfg, tool: doc.ToolSelect,
		styles: map[doc.Tool]doc.Style{}, zoom: 1, title: "截图",
		exportFormat: render.PNG, exportQuality: 0.92,
	}
	for _, t := range doc.DrawTools {
		a.styles[t] = doc.DefaultStyle(t)
	}
	return a
}

// Open creates a window only after a complete image has been decoded. It is
// sized before being shown, so neither startup nor failed opens create a home.
func (a *App) Open(parent context.Context, picture Picture, target *mygo.Rectangle, token string) *mygo.Window {
	if a.closed || a.win != nil || picture.pixels == nil {
		return nil
	}
	a.cancel()
	a.ctx, a.cancel = context.WithCancel(parent)
	a.loadPicture(picture, target)
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title: a.title + " — " + brand.Name, Width: minWidth, Height: minHeight,
		MinWidth: minWidth, MinHeight: minHeight, UseContentSize: true,
		Frameless: true, DisableMaximize: true, DisableFullScreen: true,
		Hidden: true, Content: ui.View(a.view),
	})
	a.win.OnClosed(a.release)
	a.zoom = a.fitZoom()
	a.fitWindow(true)
	withActivationToken(token, a.Show)
	return a.win
}

func (a *App) loadPicture(picture Picture, target *mygo.Rectangle) {
	a.lease.Release()
	a.lease = picture.lease.Retain()
	im := picture.image
	a.image, a.pixels, a.mosaicPx = &im, picture.pixels, picture.mosaic
	a.title, a.target = picture.title, target
	a.selection = picture.selection
	a.bitmap = ui.NewBitmap(render.Preview(a.pixels, 4096))
	if a.mosaicPx != nil {
		a.mosaic = ui.NewBitmap(a.mosaicPx).Pixelated()
	}
}

func (a *App) Window() *mygo.Window        { return a.win }
func (a *App) SetConfig(cfg config.Config) { a.cfg = cfg }

// release is also run for a window-manager close and application quit.
func (a *App) release() {
	if a.closed {
		return
	}
	a.closed = true
	a.cancel()
	a.ocrRunning, a.ocrCancel = false, nil
	a.OnOCRResult = nil
	a.OnOCRStart = nil
	a.OnCapture, a.OnScroll, a.OnSettings, a.OnRecord, a.OnGIF = nil, nil, nil, nil, nil
	a.image, a.pixels, a.mosaicPx = nil, nil, nil
	a.bitmap, a.mosaic = nil, nil
	a.lease.Release()
	a.lease = nil
	a.history = doc.History{}
	a.drag, a.styles, a.toasts = nil, nil, nil
	a.selected, a.editingID, a.editingText, a.slidingKey, a.title = "", "", "", "", ""
	a.annotationText, a.styleText = ui.Handle{}, ui.Handle{}
	a.styleHidden = false
	a.target, a.panel, a.lastPanel = nil, PanelNone, PanelNone
	a.selection = nil
	a.busy, a.pinned, a.textEdit, a.editFresh = false, false, false, false
	done := a.OnClosed
	a.OnClosed, a.OnPaste = nil, nil
	if done != nil {
		done()
	}
}

func (a *App) accept(fn func()) {
	if a.closed || a.ctx.Err() != nil {
		return
	}
	fn()
	if !a.closed && a.win != nil {
		a.win.Invalidate()
	}
}

func (a *App) later(fn func()) { mygo.RunOnMain(func() { a.accept(fn) }) }

func (a *App) toast(message string) {
	if a.closed {
		return
	}
	a.toasts = append(a.toasts, message)
	if a.win != nil {
		a.win.Invalidate()
	}
}

func (a *App) Show() {
	if a.closed || a.win == nil {
		return
	}
	a.win.Show()
	a.win.Focus()
}

// Close discards this image immediately. There is no project or hidden image
// window to restore, and a close never asks about unsaved annotations.
func (a *App) Close() {
	if a.win != nil {
		a.win.Close()
	} else {
		a.release()
	}
}

func (a *App) fitZoom() float64 {
	if a.image == nil {
		return 1
	}
	area := a.workArea()
	w := math.Max(120, float64(area.Width)-railWidth-24)
	h := math.Max(120, float64(area.Height)-topBarHeight-24)
	z := math.Min(1, math.Min(w/float64(a.image.Width), h/float64(a.image.Height)))
	return roundZoom(clamp(z, minZoom, 1))
}

func (a *App) workArea() mygo.Rectangle {
	if a.target != nil {
		return mygo.Screen.DisplayMatching(*a.target).WorkArea
	}
	return mygo.Screen.DisplayMatching(a.win.Bounds()).WorkArea
}

func roundZoom(z float64) float64     { return math.Round(z*100) / 100 }
func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func (a *App) setZoom(z float64) {
	a.zoom = roundZoom(clamp(z, minZoom, maxZoom))
	if a.win != nil {
		a.fitWindow(false)
	}
}

func (a *App) zoomToScreen() {
	a.target = nil
	a.setZoom(a.fitZoom())
}

type layoutKey struct{ w, h int }

func (a *App) fitWindow(center bool) {
	if a.closed || a.win == nil {
		return
	}
	w, h := a.wantedSize()
	key := layoutKey{w, h}
	if key == a.layout && !center {
		return
	}
	a.layout = key
	a.win.SetContentSize(w, h)
	if center {
		area := a.workArea()
		a.win.SetPosition(area.X+max(0, (area.Width-w)/2), area.Y+max(0, (area.Height-h)/2))
	}
}

func (a *App) wantedSize() (int, int) {
	if a.pinned {
		area := a.workArea()
		w := min(max(pinMinSize, int(math.Round(float64(a.image.Width)*a.zoom))), area.Width)
		h := min(max(pinMinSize, int(math.Round(float64(a.image.Height)*a.zoom))), area.Height)
		return w, h
	}
	w := int(math.Round(float64(a.image.Width)*a.zoom)) + railWidth
	h := int(math.Round(float64(a.image.Height)*a.zoom)) + topBarHeight
	if a.panel == PanelStyle {
		h = max(h, styleHeight+topBarHeight)
	}
	area := a.workArea()
	w = min(max(w, minWidth), max(minWidth, area.Width-24))
	h = min(max(h, minHeight), max(minHeight, area.Height-24))
	return w, h
}

func safeFileName(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return '-'
		}
		return r
	}, s))
	if s == "" {
		return brand.Name
	}
	return s
}
