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

func (v *scrollWindow) openOverlay(display mygo.Display, region image.Rectangle, sourceSize image.Point) {
	b := display.Bounds
	sx, sy := float32(b.Width)/float32(sourceSize.X), float32(b.Height)/float32(sourceSize.Y)
	v.overlayRegion = ui.Rect{X: float32(region.Min.X) * sx, Y: float32(region.Min.Y) * sy, W: float32(region.Dx()) * sx, H: float32(region.Dy()) * sy}
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

func (v *scrollWindow) overlayView(c *ui.Context) {
	panelTheme(c)
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
	if v.overlayToolbar.W >= 400 && v.overlayToolbar.H <= 60 {
		ui.Row(c).Fill().Padding(8).Gap(6).AlignItems(ui.Center).Background(c.Theme().Background).Children(func() {
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
			ui.Text(c, label).FontSize(12).FontWeight(600).Grow(1).
				Tooltip(v.notice)
			v.overlayActions(c)
		})
		return
	}
	ui.Column(c).Fill().Padding(10).Gap(6).Background(c.Theme().Background).Children(func() {
		ui.Text(c, fmt.Sprintf("长截图 · %d px", v.height)).FontWeight(600)
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
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Text(c, status).FontSize(12).TextColor(c.Theme().TextMuted)
		})
		ui.Row(c).Gap(6).Wrap().Justify(ui.End).Children(func() { v.overlayActions(c) })
	})
}

func (v *scrollWindow) overlayActions(c *ui.Context) {
	if ui.Button(c, "取消").Radius(6).Disabled(v.ending).Clicked() {
		v.win.Close()
	}
	if ui.Button(c, "撤回").Radius(6).Disabled(!v.canUndo || v.ending).Clicked() {
		v.paused = true
		v.send("undo")
	}
	if v.paused {
		if ui.Button(c, "继续").Radius(6).Disabled(v.ending || v.sourceEnded).Clicked() {
			v.resume()
		}
	} else if ui.Button(c, "暂停").Radius(6).Disabled(v.ending).Clicked() {
		v.paused = true
		v.send("pause")
	}
	if ui.PrimaryButton(c, "完成").Radius(6).Disabled(v.ending).Clicked() {
		v.finish()
	}
}
