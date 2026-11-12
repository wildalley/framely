package desktop

import (
	"fmt"
	"github.com/wildalley/framely/internal/brand"
	"image"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// toolbarBeside never places controls inside the pixels being captured.
func toolbarBeside(region ui.Rect, width, height float32) (ui.Rect, bool) {
	// Mutter can draw compositor shadows even for an undecorated window.
	const gap = float32(32)
	// Anchor to the selection's lower right instead of its upper left.
	// Try all compact sizes below first, so a tall page does not send the
	// controls a screen's height away merely to fit a larger status panel.
	for side := range 4 {
		for _, size := range []ui.Point{{X: 420, Y: 52}, {X: 360, Y: 100}, {X: 190, Y: 194}} {
			w, h := size.X, size.Y
			x := max(gap, min(region.X+region.W-w, width-w-gap))
			y := max(gap, min(region.Y+region.H-h, height-h-gap))
			candidates := [4]ui.Rect{
				{X: x, Y: region.Y + region.H + gap, W: w, H: h},
				{X: region.X + region.W + gap, Y: y, W: w, H: h},
				{X: region.X - w - gap, Y: y, W: w, H: h},
				{X: x, Y: region.Y - h - gap, W: w, H: h},
			}
			r := candidates[side]
			if r.X >= 0 && r.Y >= 0 && r.X+r.W <= width && r.Y+r.H <= height {
				return r, true
			}
		}
	}
	return ui.Rect{}, false
}

func rectsOverlap(a, b ui.Rect) bool {
	return a.X < b.X+b.W && a.X+a.W > b.X && a.Y < b.Y+b.H && a.Y+a.H > b.Y
}

func expandRect(r ui.Rect, pad float32) ui.Rect {
	return ui.Rect{X: r.X - pad, Y: r.Y - pad, W: r.W + pad*2, H: r.H + pad*2}
}

// previewBeside places a live preview card outside both the captured region
// and the control bar.
func previewBeside(region, toolbar ui.Rect, width, height float32) (ui.Rect, bool) {
	const gap = float32(32)
	const barGap = float32(12)
	avoidRegion := expandRect(region, gap)
	avoidBar := expandRect(toolbar, barGap)
	for _, size := range []ui.Point{{X: 210, Y: 300}, {X: 190, Y: 240}, {X: 170, Y: 180}, {X: 200, Y: 140}} {
		w, h := size.X, size.Y
		topY := max(gap, min(region.Y, height-h-gap))
		botY := max(gap, min(region.Y+region.H-h, height-h-gap))
		rightX := max(gap, min(region.X+region.W-w, width-w-gap))
		leftX := max(gap, min(region.X, width-w-gap))
		candidates := []ui.Rect{
			{X: region.X + region.W + gap, Y: botY, W: w, H: h},
			{X: region.X + region.W + gap, Y: topY, W: w, H: h},
			{X: region.X - w - gap, Y: botY, W: w, H: h},
			{X: region.X - w - gap, Y: topY, W: w, H: h},
			{X: toolbar.X - w - barGap, Y: region.Y + region.H + gap, W: w, H: h},
			{X: leftX, Y: region.Y + region.H + gap, W: w, H: h},
			{X: rightX, Y: region.Y - h - gap, W: w, H: h},
			{X: leftX, Y: region.Y - h - gap, W: w, H: h},
		}
		for _, r := range candidates {
			if r.X >= 0 && r.Y >= 0 && r.X+r.W <= width && r.Y+r.H <= height &&
				!rectsOverlap(r, avoidRegion) && (toolbar.W <= 0 || !rectsOverlap(r, avoidBar)) {
				return r, true
			}
		}
	}
	return ui.Rect{}, false
}

func (v *scrollWindow) openOverlay(display mygo.Display, region image.Rectangle, sourceSize image.Point) {
	b := display.Bounds
	sx, sy := float32(b.Width)/float32(sourceSize.X), float32(b.Height)/float32(sourceSize.Y)
	v.overlayRegion = ui.Rect{X: float32(region.Min.X) * sx, Y: float32(region.Min.Y) * sy, W: float32(region.Dx()) * sx, H: float32(region.Dy()) * sy}
	if v.regionFrame == nil {
		if fw, _, hole, ok := openRegionFrame(v.ctx, brand.Title("长截图选区"), display, v.overlayRegion, v.regionFrameView, v.app.worker); ok {
			v.regionFrame, v.frameHole = fw, hole
		}
	}
	toolbar, ok := toolbarBeside(v.overlayRegion, float32(b.Width), float32(b.Height))
	if !ok {
		v.notice = "选区周围没有空间放置按钮；滚动后按截图快捷键完成。"
		return
	}
	v.overlayToolbar = toolbar
	w, err := openControlBar(v.ctx, brand.Title("长截图控件"), display, toolbar, v.overlayView, v.app.worker)
	if err != nil {
		v.notice = err.Error() + "；滚动后按截图快捷键完成。"
		return
	}
	v.overlay, v.floating = w, true
	if previewRect, ok := previewBeside(v.overlayRegion, toolbar, float32(b.Width), float32(b.Height)); ok {
		if pw, err := openPassiveBar(v.ctx, brand.Title("长截图预览"), display, previewRect, v.previewOverlayView, v.app.worker); err == nil {
			v.previewOverlay, v.overlayPreview = pw, previewRect
		}
	}
	input := w.Bounds()
	w.OnClose(func(*mygo.CloseEvent) { v.win.Close() })
	v.app.worker(func() {
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-v.ctx.Done():
			return
		case <-timer.C:
		}
		v.later(func() {
			if !w.IsDestroyed() {
				log.Printf(brand.Name+": long screenshot toolbar: expected=%v actual=%v", input, w.Bounds())
			}
		})
	})
}

func (v *scrollWindow) regionFrameView(c *ui.Context) {
	panelTheme(c)
	t := c.Theme()
	drawRegionFrame(c, v.frameHole, v.overlayStatusDot(t))
}

func (v *scrollWindow) previewOverlayView(c *ui.Context) {
	panelTheme(c)
	c.Root().Background(ui.Transparent)
	if v.closed {
		return
	}
	t := c.Theme()
	dot := v.overlayStatusDot(t)
	ui.Column(c).Fill().Padding(10).Gap(8).Background(t.Background).
		Border(1, t.Border).Radius(12).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
			ui.Text(c, "实时预览").FontSize(12).FontWeight(600).Grow(1).SingleLine()
			ui.Text(c, fmt.Sprintf("%d px", v.height)).Font("monospace").FontSize(11).TextColor(t.TextMuted).SingleLine()
		})
		ui.Box(c.Key("scroll-live-preview")).Grow(1).MinHeight(0).FillWidth().
			Radius(8).Clip().Background(t.Surface).Border(1, t.Border).
			Draw(func(p *ui.Painter, r ui.Rect) {
				if v.preview != nil {
					inner := ui.Rect{X: r.X + 4, Y: r.Y + 4, W: max(0, r.W-8), H: max(0, r.H-8)}
					v.drawPreview(p, inner)
				}
			}).Children(func() {
			if v.preview == nil {
				ui.Box(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					ui.Text(c, "滚动后实时拼接").FontSize(11).TextColor(t.TextMuted).SingleLine()
				})
			}
		})
	})
}

func (v *scrollWindow) overlayStatusDot(t *ui.Theme) ui.Color {
	switch {
	case v.ending:
		return brand.Accent(t.Dark)
	case v.paused || v.sourceEnded:
		return t.TextMuted
	default:
		return brand.Signal
	}
}

func (v *scrollWindow) overlayView(c *ui.Context) {
	panelTheme(c)
	c.Root().Background(ui.Transparent)
	if v.closed {
		return
	}
	if c.Shortcut(0, ui.KeyEscape) {
		v.win.Close()
		return
	}
	if c.Shortcut(0, ui.KeyEnter) {
		v.finish()
		return
	}
	t := c.Theme()
	dot := v.overlayStatusDot(t)
	if v.overlayToolbar.W >= 400 && v.overlayToolbar.H <= 60 {
		ui.Row(c).Fill().Padding(8, 10).Gap(6).AlignItems(ui.Center).Background(t.Background).
			Border(1, t.Border).Radius(12).Children(func() {
			label := fmt.Sprintf("长截图 · %d px", v.height)
			switch {
			case v.ending:
				label = "正在生成长截图…"
			case v.sourceEnded:
				label = "采集已断开"
			case v.paused:
				label = fmt.Sprintf("已暂停 · %d px", v.height)
			case v.notice != "":
				label = "请放慢滚动"
			}
			ui.Row(c).Grow(1).MinWidth(0).Gap(7).AlignItems(ui.Center).Children(func() {
				if v.previewOverlay == nil && v.preview != nil {
					ui.Box(c.Key("bar-mini-preview")).Size(28, 32).Radius(6).Clip().
						Background(t.Surface).Border(1, t.Border).Shrink(0).
						Draw(func(p *ui.Painter, r ui.Rect) {
							if v.preview != nil {
								v.drawPreview(p, ui.Rect{X: r.X + 2, Y: r.Y + 2, W: max(0, r.W-4), H: max(0, r.H-4)})
							}
						})
				}
				ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
				ui.Text(c, label).FontSize(12).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
			})
			ui.Box(c).Size(1, 22).Background(t.Border).Shrink(0)
			v.overlayActions(c)
		})
		return
	}
	ui.Column(c).Fill().Padding(10).Gap(6).Background(t.Background).
		Border(1, t.Border).Radius(12).Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
			ui.Text(c, fmt.Sprintf("长截图 · %d px", v.height)).FontWeight(600)
		})
		status := "在选区内滚动或拖动滚动条，点完成出图"
		if v.automatic {
			status = "正在自动向下滚动，点完成出图"
		}
		if v.paused {
			status = "已暂停，点继续恢复采集"
		}
		if v.notice != "" {
			status = v.notice
		}
		if v.ending {
			status = "正在生成长截图…"
		}
		ui.Row(c).Grow(1).MinHeight(0).Gap(8).AlignItems(ui.Center).Children(func() {
			if v.previewOverlay == nil && v.preview != nil {
				ui.Box(c.Key("card-mini-preview")).Size(44, 44).Radius(6).Clip().
					Background(t.Surface).Border(1, t.Border).Shrink(0).
					Draw(func(p *ui.Painter, r ui.Rect) {
						if v.preview != nil {
							v.drawPreview(p, ui.Rect{X: r.X + 2, Y: r.Y + 2, W: max(0, r.W-4), H: max(0, r.H-4)})
						}
					})
			}
			ui.Scroll(c).Grow(1).MinHeight(0).FillHeight().Children(func() {
				ui.Text(c, status).FontSize(12).TextColor(t.TextMuted)
			})
		})
		ui.Row(c).Gap(6).Wrap().Justify(ui.End).Children(func() { v.overlayActions(c) })
	})
}

func (v *scrollWindow) overlayActions(c *ui.Context) {
	if ui.Button(c, "取消").Height(32).Padding(0, 10).Radius(8).Disabled(v.ending).Clicked() {
		v.win.Close()
	}
	if ui.Button(c, "撤回").Height(32).Padding(0, 10).Radius(8).Disabled(!v.canUndo || v.ending).Clicked() {
		v.paused = true
		v.invalidateOverlays()
		v.send("undo")
	}
	if v.paused {
		if ui.Button(c, "继续").Height(32).Padding(0, 10).Radius(8).Disabled(v.ending || v.sourceEnded).Clicked() {
			v.resume()
		}
	} else if ui.Button(c, "暂停").Height(32).Padding(0, 10).Radius(8).Disabled(v.ending).Clicked() {
		v.paused = true
		v.invalidateOverlays()
		v.send("pause")
	}
	if ui.PrimaryButton(c, "完成").Height(32).Padding(0, 12).Radius(8).Disabled(v.ending).Clicked() {
		v.finish()
	}
}
