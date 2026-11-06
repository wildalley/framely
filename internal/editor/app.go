// Package editor is ScreenPin's window: the pinned screenshot, its toolbar
// and panels, and the region picker of a capture.
package editor

import (
	"errors"
	"image"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/config"
	"github.com/wildalley/screenpin/internal/doc"
	"github.com/wildalley/screenpin/internal/render"
)

// Panel is the popover open over the editor.
type Panel int

const (
	PanelNone Panel = iota
	PanelStyle
	PanelMenu
	PanelShortcuts
	PanelSettings
)

// Zoom limits, and the window's smallest size.
const (
	minZoom     = 0.1
	maxZoom     = 3
	minWidth    = 320
	emptyWidth  = 460
	emptyHeight = 300
)

// App is the editor's state. Its fields belong to the main thread, where
// the view builds from them; goroutines change them through Window.Update.
type App struct {
	win *mygo.Window

	cfg config.Config

	// The document.
	image     *doc.Image
	pixels    *image.RGBA // decoded image, kept for export and the mosaic
	mosaicPx  *image.RGBA
	bitmap    *ui.Bitmap
	mosaic    *ui.Bitmap
	title     string
	createdAt string
	history   doc.History
	saved     string // fingerprint of what was last saved or loaded

	tool     doc.Tool
	selected string
	styles   map[doc.Tool]doc.Style
	zoom     float64
	pinned   bool
	busy     bool
	panel    Panel
	toasts   []string

	exportFormat  render.Format
	exportQuality float64

	// The canvas's drag, and the text being edited in place.
	drag        *drag
	hoverCursor ui.Cursor
	editingID   string
	editingText string
	editFresh   bool

	// slidingKey is the style slider holding a history transaction.
	slidingKey string
	textEdit   bool // the style panel's text field holds one

	// displayZoom is the zoom the canvas shows at, which the window's
	// actual size may hold below zoom.
	displayZoom float64
	panelHeight float32
	lastPanel   Panel

	// Settings panel.
	settings settingsState

	// Where the last capture was picked, to open the editor on that display.
	target *mygo.Rectangle

	capturing bool
	quitting  bool
	// layout is what the window was last sized for.
	layout layoutKey

	// Hooks into the app around the editor.
	OnShortcutChanged func(old, new string) error
	OnAutostartChanged func(bool)
}

// New returns the editor's state, with cfg the loaded configuration.
func New(cfg config.Config) *App {
	a := &App{
		cfg:           cfg,
		tool:          doc.ToolSelect,
		styles:        map[doc.Tool]doc.Style{},
		zoom:          1,
		title:         "未命名截图",
		exportFormat:  render.PNG,
		exportQuality: 0.92,
	}
	for _, t := range doc.DrawTools {
		a.styles[t] = doc.DefaultStyle(t)
	}
	return a
}

// Config returns the current configuration.
func (a *App) Config() config.Config { return a.cfg }

// Open creates the editor's window, hidden when hidden is true.
func (a *App) Open(hidden bool) *mygo.Window {
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           "ScreenPin",
		Width:           emptyWidth,
		Height:          emptyHeight,
		UseContentSize:  true,
		Frameless:       true,
		DisableMaximize: true,
		DisableFullScreen: true,
		Hidden:          hidden,
		BackgroundColor: "#fafaf8",
		Content:         ui.View(a.view),
	})
	// Closing the window keeps the app in the tray, as the X button does.
	a.win.OnClose(func(e *mygo.CloseEvent) {
		if a.quitting {
			return
		}
		e.PreventDefault()
		a.hide()
	})
	return a.win
}

// Window returns the editor's window.
func (a *App) Window() *mygo.Window { return a.win }

// toast shows a message at the next frame.
func (a *App) toast(msg string) {
	a.toasts = append(a.toasts, msg)
	a.win.Invalidate()
}

// later runs fn on the main thread and redraws.
func (a *App) later(fn func()) { a.win.Update(fn) }

// Show brings the editor forward.
func (a *App) Show() {
	a.win.Show()
	a.win.Focus()
}

func (a *App) hide() {
	a.panel = PanelNone
	a.win.Hide()
}

// dirty reports whether the document has changes no save holds.
func (a *App) dirty() bool {
	return a.image != nil && doc.Fingerprint(a.title, a.createdAt, a.image, a.history.Present()) != a.saved
}

func (a *App) markSaved() {
	a.saved = doc.Fingerprint(a.title, a.createdAt, a.image, a.history.Present())
}

// confirmDiscard asks, off the main thread, whether changes may be lost.
// dirty is read on the main thread before.
func (a *App) confirmDiscard(dirty bool) bool {
	if !dirty {
		return true
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.win,
		Type:               mygo.MessageWarning,
		Title:              "放弃未保存的修改？",
		Message:            "放弃未保存的修改？",
		Detail:             "当前工程有未保存的修改，继续将丢失这些修改。",
		Buttons:            []string{"取消", "继续"},
		DefaultButton:      0,
		CancelButton:       0,
		DestructiveButtons: []int{1},
	})
	return err == nil && res.Button == 1
}

// RequestQuit quits once unsaved changes are confirmed away.
func (a *App) RequestQuit() {
	dirty := a.dirty()
	if dirty {
		a.Show()
	}
	go func() {
		if !a.confirmDiscard(dirty) {
			return
		}
		a.later(func() {
			a.quitting = true
			mygo.App.Quit()
		})
	}()
}

// AllowQuit lets the window close for a quit that did not come from
// RequestQuit, as the system's logout.
func (a *App) AllowQuit() { a.quitting = true }

// loaded is a decoded document ready to show.
type loaded struct {
	image     doc.Image
	pixels    *image.RGBA
	mosaic    *image.RGBA
	title     string
	createdAt string
	list      []doc.Annotation
}

// decode decodes an image and pixelates it, off the main thread.
func decode(im doc.Image) (loaded, error) {
	px, err := capture.Decode(im.Data)
	if err != nil {
		return loaded{}, err
	}
	if px.Bounds().Dx() != im.Width || px.Bounds().Dy() != im.Height {
		return loaded{}, errors.New("图片尺寸与工程数据不一致")
	}
	return loaded{image: im, pixels: px, mosaic: render.Pixelate(px)}, nil
}

// show replaces the document with l, on the main thread.
func (a *App) show(l loaded) {
	im := l.image
	a.image = &im
	a.pixels, a.mosaicPx = l.pixels, l.mosaic
	a.bitmap = ui.NewBitmap(l.pixels)
	a.mosaic = nil
	if l.mosaic != nil {
		a.mosaic = ui.NewBitmap(l.mosaic).Pixelated()
	}
	a.title, a.createdAt = l.title, l.createdAt
	a.history.Reset(l.list)
	a.selected, a.tool, a.panel = "", doc.ToolSelect, PanelNone
	a.drag, a.editingID = nil, ""
	a.markSaved()
	a.zoom = a.fitZoom()
	a.fitWindow(true)
}

// loadImage shows encoded image data as a fresh, unsaved capture.
func (a *App) loadImage(data []byte, name string) error {
	im, err := doc.DecodeImage(data, name)
	if err != nil {
		return err
	}
	l, err := decode(im)
	if err != nil {
		return err
	}
	l.title = strings.TrimSuffix(name, filepath.Ext(name))
	l.createdAt = doc.Timestamp(time.Now())
	done := make(chan struct{})
	a.later(func() { a.show(l); close(done) })
	<-done
	return nil
}

func captureName() string { return "截图 " + time.Now().Format("15:04") }

// fitZoom is the largest zoom up to 100% that keeps the image and the bar
// inside the work area of the display the editor opens on.
func (a *App) fitZoom() float64 {
	if a.image == nil {
		return 1
	}
	area := a.workArea()
	w := math.Max(120, float64(area.Width)-24)
	h := math.Max(120, float64(area.Height)-float64(barHeightFor(stageWidth(a.image.Width, 1)))-24)
	z := math.Min(1, math.Min(w/float64(a.image.Width), h/float64(a.image.Height)))
	return roundZoom(clamp(z, minZoom, 1))
}

func (a *App) workArea() mygo.Rectangle {
	if a.target != nil {
		return mygo.Screen.DisplayMatching(*a.target).WorkArea
	}
	return mygo.Screen.DisplayMatching(a.win.Bounds()).WorkArea
}

func roundZoom(z float64) float64 { return math.Round(z*100) / 100 }

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func (a *App) setZoom(z float64) {
	a.zoom = roundZoom(clamp(z, minZoom, maxZoom))
	a.fitWindow(false)
}

func (a *App) zoomToScreen() {
	a.target = nil
	a.setZoom(a.fitZoom())
}

// layoutKey is what decides the window's size.
type layoutKey struct{ w, h int }

// fitWindow sizes the window to the image at the zoom, the bar and the open
// panel, and centers it on its display when center is true.
func (a *App) fitWindow(center bool) {
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
	if a.image == nil {
		return emptyWidth, emptyHeight
	}
	sw := stageWidth(a.image.Width, a.zoom)
	sh := int(math.Round(float64(a.image.Height) * a.zoom))
	w := max(minWidth, sw)
	if a.panel != PanelNone {
		w = max(w, panelWidth(a.panel)+panelMargin)
	}
	bar := barHeightFor(w)
	h := sh
	if a.panel != PanelNone && a.panelHeight > 0 {
		h = max(h, int(a.panelHeight)+panelMargin)
	}
	return w, h + bar
}

func stageWidth(width int, zoom float64) int { return int(math.Round(float64(width) * zoom)) }

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
