package desktop

import (
	"context"
	"log"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/wildalley/framely/internal/brand"
	"github.com/wildalley/framely/internal/capture"
	"github.com/wildalley/framely/internal/editor"
	"github.com/wildalley/framely/internal/scrollshot"
)

func (v *scrollWindow) runScrollCapture(ctx context.Context, request scrollRequest) {
	prepared, err := v.prepareScrollCapture(ctx, request)
	if err != nil {
		v.startFailed(err)
		return
	}
	defer prepared.Close()
	controller, err := scrollshot.NewController(prepared.baseline, prepared.automatic, time.Now())
	prepared.baseline = nil // Do not retain the full selection frame while capturing.
	if err != nil {
		v.startFailed(err)
		return
	}
	defer controller.Stitch.Release()
	slot, err := editor.ReservePicture(controller.Stitch.MaxPixels)
	if err != nil {
		v.startFailed(err)
		return
	}
	defer slot.Close()
	if prepared.fallback {
		controller.Notice = "已切换手动模式，请缓慢向下滚动，按截图快捷键完成。"
	}
	if err := capture.SetStreamRegion(ctx, prepared.Stream, prepared.region, prepared.size); err != nil {
		v.startFailed(err)
		return
	}
	// Install controls before any queued progress snapshots can reach them.
	controlsNotice := ""
	mygo.RunOnMain(func() {
		if !v.closed && ctx.Err() == nil {
			controlsNotice = v.scrollStarted(prepared)
		}
	})
	if controlsNotice != "" {
		controller.Notice = controlsNotice
	}
	session := scrollshot.Session{Controller: controller, Region: prepared.region, SourceSize: prepared.size,
		Commands: v.commands, Progress: v.applyScrollProgress}
	defer session.Close()
	if !session.Run(ctx, prepared) {
		if ctx.Err() == nil {
			v.later(func() { v.win.Destroy() })
		}
		return
	}
	if ctx.Err() != nil {
		return
	}
	pixels := controller.Stitch.Flatten()
	log.Printf(brand.Name+": long screenshot completed: %dx%d", pixels.Rect.Dx(), pixels.Rect.Dy())
	picture, err := slot.FromPixels(pixels, "长截图 "+time.Now().Format("15-04-05"))
	defer picture.Close()
	// The source remains alive until the editor handoff is queued.
	v.later(func() { v.showScrollPicture(picture, err) })
}

func (v *scrollWindow) scrollStarted(prepared *scrollCapture) string {
	v.busy, v.running, v.autoGranted, v.notice = false, true, prepared.automatic, ""
	if prepared.screen != nil {
		v.display = *prepared.screen
	}
	if runtime.GOOS == "linux" && prepared.screen != nil {
		v.openOverlay(*prepared.screen, prepared.region, prepared.size)
	} else if capture.StreamSourceType(prepared.Stream) == 2 {
		v.floating = true // A controller is never part of a window-only stream.
	}
	if v.floating && v.overlay == nil {
		area := v.display.WorkArea
		v.win.SetBounds(mygo.Rectangle{X: area.X + max(0, area.Width-440), Y: area.Y + 16, Width: 420, Height: 170})
		v.win.ShowInactive()
	}
	log.Printf(brand.Name+": long screenshot selection: source=%d region=%v overlay=%t floating=%t", capture.StreamSourceType(prepared.Stream), prepared.region, v.overlay != nil, v.floating)
	v.app.refreshTray()
	return v.notice
}

func (v *scrollWindow) showScrollPicture(picture editor.Picture, err error) {
	if err != nil {
		v.app.notify("长截图生成失败：" + err.Error())
		v.win.Destroy()
		return
	}
	v.app.operations++ // Keep no-tray mode alive during the window handoff.
	v.win.Destroy()
	target := v.display.Bounds
	v.app.showPicture(picture, &target, "")
	log.Print(brand.Name + ": long screenshot editor opened")
	v.app.operations--
	v.app.maybeQuit()
}

func (v *scrollWindow) applyScrollProgress(progress scrollshot.Progress) {
	log.Printf(brand.Name+": long screenshot progress: height=%d paused=%t notice=%q", progress.Height, progress.Paused, progress.Notice)
	v.later(func() {
		if progress.Preview != nil {
			v.setPreview(progress.Preview)
		}
		v.height, v.canUndo, v.paused = progress.Height, progress.CanUndo, progress.Paused
		v.automatic, v.notice, v.sourceEnded = progress.Automatic, progress.Notice, progress.SourceEnded
		if v.overlay != nil {
			v.invalidateOverlays()
		} else if progress.Show {
			v.win.Show()
			v.win.Focus()
		}
	})
}
