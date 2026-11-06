// Package editor owns temporary image windows and the screenshot region picker.
package editor

import (
	"context"
	"image"
	"math"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/config"
	"github.com/wildalley/screenpin/internal/doc"
	"github.com/wildalley/screenpin/internal/render"
)

type Panel int

const (
	PanelNone Panel = iota
	PanelStyle
)

const (
	minZoom     = 0.1
	maxZoom     = 3
	minWidth    = 320
	barHeight   = 44
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

	tool      doc.Tool
	selected  string
	styles    map[doc.Tool]doc.Style
	zoom      float64
	pinned    bool
	busy      bool
	panel     Panel
	lastPanel Panel
	toasts    []string

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

	target *mygo.Rectangle
	layout layoutKey

	OnClosed func()
	OnPaste  func()
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
	im := picture.image
	a.image, a.pixels, a.mosaicPx = &im, picture.pixels, picture.mosaic
	a.title, a.target = picture.title, target
	a.bitmap = ui.NewBitmap(a.pixels)
	if a.mosaicPx != nil {
		a.mosaic = ui.NewBitmap(a.mosaicPx).Pixelated()
	}
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title: a.title + " — ScreenPin", Width: minWidth, Height: barHeight + 80,
		MinWidth: minWidth, MinHeight: barHeight + 40, UseContentSize: true,
		Frameless: true, DisableMaximize: true, DisableFullScreen: true,
		Hidden: true, Content: ui.View(a.view),
	})
	a.win.OnClosed(a.release)
	a.zoom = a.fitZoom()
	a.fitWindow(true)
	withActivationToken(token, a.Show)
	return a.win
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
	a.image, a.pixels, a.mosaicPx = nil, nil, nil
	a.bitmap, a.mosaic = nil, nil
	a.history = doc.History{}
	a.drag, a.styles, a.toasts = nil, nil, nil
	a.selected, a.editingID, a.editingText, a.slidingKey, a.title = "", "", "", "", ""
	a.annotationText, a.styleText = ui.Handle{}, ui.Handle{}
	a.target, a.panel, a.lastPanel = nil, PanelNone, PanelNone
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
	w := math.Max(120, float64(area.Width)-24)
	h := math.Max(120, float64(area.Height)-barHeight-24)
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
	w := max(minWidth, int(math.Round(float64(a.image.Width)*a.zoom)))
	h := max(40, int(math.Round(float64(a.image.Height)*a.zoom)))
	if a.panel == PanelStyle {
		w, h = max(w, styleWidth+16), max(h, styleHeight)
	}
	area := a.workArea()
	return min(w, max(minWidth, area.Width-24)), min(h+barHeight, max(barHeight+40, area.Height-24))
}

func safeFileName(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return '-'
		}
		return r
	}, s))
	if s == "" {
		return "screenpin"
	}
	return s
}
