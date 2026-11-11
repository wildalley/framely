package editor

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/brand"
	"github.com/wildalley/framely/internal/doc"
	"github.com/wildalley/framely/internal/render"
)

// A pinned image is the image alone, on top of other windows, with a thin
// outline: dragging it moves the window, the wheel zooms it, a double-click
// goes back to editing, and resting the pointer on it shows a small bar of
// edit, copy, zoom and close.
const (
	pinMinSize = 24
	// capsuleMinWidth is the narrowest image that gets the hover bar; the
	// context menu offers the same actions on smaller ones.
	capsuleMinWidth = 132
)

func (a *App) pinnedView(c *ui.Context) {
	ww, wh := c.Size()
	z := math.Min(a.zoom, math.Min(float64(max(1, ww))/float64(a.image.Width), float64(max(1, wh))/float64(a.image.Height)))
	a.displayZoom = z
	scene := &render.Scene{
		Image:       a.bitmap,
		Mosaic:      a.mosaic,
		Width:       a.image.Width,
		Height:      a.image.Height,
		Annotations: a.history.Present(),
		CornerRatio: a.history.ImageCornerRatio(),
	}
	root := ui.Box(c.Key("pinned")).Label("钉住的图片").Fill().DragWindow().ContextMenu(a.contextMenu)
	root.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerDown:
			if ev.Button == 0 && ev.Clicks >= 2 {
				a.TogglePin()
				return true
			}
		case ui.InputScroll:
			if ev.DY != 0 {
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
	})
	root.Draw(func(p *ui.Painter, r ui.Rect) {
		s := float32(z)
		p.Clip(r, 0, func() {
			p.Transform(ui.Scaling(s, s).Then(ui.Translation(r.X, r.Y)), 0, 0, func() {
				render.Draw(p, scene)
			})
		})
		p.Stroke(r, brand.Signal.Alpha(.85), 0, 1)
	})
	root.Children(func() {
		if root.Hovered() && ww >= capsuleMinWidth && wh >= 48 {
			a.pinCapsule(c)
		}
	})
}

func (a *App) pinCapsule(c *ui.Context) {
	t := c.Theme()
	ui.Row(c.Key("pin-capsule")).Role(ui.RoleToolbar).FocusGroup(ui.Horizontal).Label("钉图操作").
		Absolute().Bottom(8).Left(0).Right(0).Margin(0, 0).Justify(ui.Center).PassThrough().Children(func() {
		ui.Row(c.Key("pin-capsule-body")).Height(36).Padding(2).Gap(2).Radius(18).AlignItems(ui.Center).
			Background(t.Background.Alpha(.94)).Border(1, t.Border).Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, .18)).
			Children(func() {
				if iconButton(c, "pin-edit", iconPen, "编辑（双击图片）", false).Radius(16).Clicked() {
					a.TogglePin()
				}
				if iconButton(c, "pin-copy", iconCopy, "复制图片 ("+commandKey()+"+C)", a.busy).Radius(16).Clicked() {
					a.Copy()
				}
				zoom := ui.Button(c.Key("pin-zoom"), fmt.Sprintf("%d%%", int(math.Round(a.displayZoom*100)))).
					Height(32).Padding(0, 6).Radius(16).FontSize(12).Font("monospace").
					Background(ui.Transparent).Border(0, ui.Transparent).
					Label("缩放到 100%").Tooltip("滚轮缩放；点击回到 100%")
				if zoom.Clicked() {
					a.setZoom(1)
				}
				if iconButton(c, "pin-close", iconClose, "关闭图片", false).Radius(16).Clicked() {
					a.Close()
				}
			})
	})
}

// pinLayout is where the image sits inside the editing window, so pinning
// keeps it in place on screen while the window's chrome goes.
func (a *App) imageOffset(w, h int) (int, int) {
	iw := int(math.Round(float64(a.image.Width) * a.zoom))
	ih := int(math.Round(float64(a.image.Height) * a.zoom))
	return railWidth + max(0, (w-railWidth-iw)/2), topBarHeight + max(0, (h-topBarHeight-ih)/2)
}

// TogglePin pins the image above other windows without chrome, or brings
// back the editor, keeping the image where it is on screen.
func (a *App) TogglePin() {
	if a.closed || a.win == nil {
		return
	}
	a.finishEditing()
	x, y := a.win.Position()
	w, h := a.wantedSize()
	ox, oy := a.imageOffset(w, h)
	a.pinned = !a.pinned
	if a.pinned {
		a.selected, a.tool, a.panel = "", doc.ToolSelect, PanelNone
		a.win.SetMinimumSize(pinMinSize, pinMinSize)
	}
	a.win.SetAlwaysOnTop(a.pinned)
	a.fitWindow(false)
	if a.pinned {
		a.win.SetPosition(x+ox, y+oy)
		return
	}
	w, h = a.wantedSize()
	ox, oy = a.imageOffset(w, h)
	a.win.SetMinimumSize(minWidth, minHeight)
	a.win.SetPosition(x-ox, y-oy)
}

// PinInPlace pins a fresh screenshot over the region it was taken from, at
// the size it showed there. Without a known region it pins in place.
func (a *App) PinInPlace() {
	if a.closed || a.win == nil {
		return
	}
	sel := a.selection
	if sel == nil || sel.Size.X <= 0 || sel.Size.Y <= 0 || sel.Region.Empty() {
		a.TogglePin()
		return
	}
	sx := float64(sel.Display.Width) / float64(sel.Size.X)
	sy := float64(sel.Display.Height) / float64(sel.Size.Y)
	a.finishEditing()
	a.zoom = roundZoom(clamp(float64(sel.Region.Dx())*sx/float64(a.image.Width), minZoom, maxZoom))
	a.pinned, a.selected, a.tool, a.panel = true, "", doc.ToolSelect, PanelNone
	a.win.SetMinimumSize(pinMinSize, pinMinSize)
	a.win.SetAlwaysOnTop(true)
	a.fitWindow(false)
	a.win.SetPosition(sel.Display.X+int(math.Round(float64(sel.Region.Min.X)*sx)), sel.Display.Y+int(math.Round(float64(sel.Region.Min.Y)*sy)))
}

// UseTool starts the image window with an annotation tool chosen.
func (a *App) UseTool(t doc.Tool) {
	if a.closed || t == "" {
		return
	}
	a.setTool(t)
}
