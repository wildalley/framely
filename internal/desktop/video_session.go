package desktop

import (
	"context"
	"image"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/brand"
	"github.com/wildalley/framely/internal/capture"
	"github.com/wildalley/framely/internal/components"
	"github.com/wildalley/framely/internal/editor"
	"github.com/wildalley/framely/internal/recording"
)

// recordingRequest is an immutable snapshot taken before starting the worker.
type recordingRequest struct {
	setup        recordingSetup
	selection    *editor.ScreenSelection
	fps, maxEdge int
	dir, token   string
	ask          bool
	commands     <-chan string
}

type recordingRegion struct {
	screen *mygo.Display
	region image.Rectangle
	size   image.Point
}

// recordingSource adapts the native protocol to the window-free session.
type recordingSource struct{ capture.Stream }

func (s recordingSource) Command(ctx context.Context, command string) error {
	return capture.RecordingCommand(ctx, s.Stream, command)
}

func (s recordingSource) Snapshot() (capture.RecordingStatus, error) {
	return capture.RecordingSnapshot(s.Stream)
}

func (v *videoWindow) recordingPath(ctx context.Context, request recordingRequest) (string, error) {
	path, err := recording.NextPath(request.dir, "录屏", ".mp4", time.Now())
	if err != nil || !request.ask {
		return path, err
	}
	path, err = mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: v.win, Title: "保存录屏视频", DefaultPath: path, Filters: []mygo.FileFilter{{Name: "MP4 视频", Extensions: []string{"mp4"}}}})
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && path != "" && !strings.EqualFold(filepath.Ext(path), ".mp4") {
		path += ".mp4"
	}
	return path, err
}

// runRecording keeps native preparation/UI handoff separate from file and
// protocol ownership in recording.Session. Every exit restores the windows.
func (v *videoWindow) runRecording(ctx context.Context, request recordingRequest) {
	var hidden []*mygo.Window
	defer v.app.dispatch(func() { v.finishRecording(hidden) })
	// Protocol probing can launch a helper: keep it on the worker goroutine.
	if _, err := components.FindProtocol("framely-record", components.FrameProtocol); err != nil {
		v.report(err)
		return
	}
	if ctx.Err() != nil {
		return
	}
	path, err := v.recordingPath(ctx, request)
	if err != nil {
		v.report(err)
		return
	}
	if path == "" {
		v.later(func() { v.notice = "" })
		return
	}
	session, err := recording.NewSession(path)
	if err != nil {
		v.report(err)
		return
	}
	defer session.Close()
	var display mygo.Display
	var displays []mygo.Display
	mygo.RunOnMain(func() {
		if v.closed || ctx.Err() != nil {
			return
		}
		displays = mygo.Screen.Displays()
		display = mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
		if request.selection != nil {
			for _, d := range displays {
				if d.Bounds == request.selection.Display {
					display = d
					break
				}
			}
		}
		for _, w := range mygo.Windows() {
			if w.IsVisible() {
				hidden = append(hidden, w)
				w.Hide()
			}
		}
	})
	if ctx.Err() != nil {
		return
	}
	options := recordingStreamOptions(display, capture.RecordingOptions{Path: session.StagedPath(), FPS: request.fps, MaxEdge: request.maxEdge})
	monitors, err := capture.StreamMonitors(ctx)
	if err != nil {
		v.report(err)
		return
	}
	options.Monitor = recordingMonitor(display, displays, monitors)
	log.Printf(brand.Name+": recording requested display: id=%d bounds=%v", display.ID, options.DisplayBounds)
	source, err := capture.OpenStream(ctx, options)
	if err != nil {
		v.report(err)
		return
	}
	transferred := false
	defer func() {
		if !transferred {
			source.Close()
		}
	}()
	selected, err := v.pickRecordingRegion(ctx, source, display, displays, request)
	if err != nil {
		v.report(err)
		return
	}
	transferred = true // Session owns the source, including all failure paths.
	result, err := session.Run(ctx, recordingSource{source}, request.commands, func(update recording.Update) {
		if update.Command == "record" {
			setup := request.setup
			setup.Selection = nil
			if selected.screen != nil {
				setup.Selection = &editor.ScreenSelection{Display: selected.screen.Bounds, Region: selected.region, Size: selected.size}
			}
			if err := saveRecordingSetup(setup); err != nil {
				log.Printf(brand.Name+": recording setup could not be remembered: %v", err)
			}
			v.later(func() { v.recordingStarted(setup, selected) })
			return
		}
		v.later(func() { v.applyRecordingUpdate(update) })
	})
	if err != nil {
		v.report(err)
		return
	}
	preview := videoPreview(ctx, result.Path)
	v.later(func() {
		if preview != nil {
			v.preview = ui.NewBitmap(preview)
		}
		v.status, v.path = result.Status, result.Path
		v.notice = "视频已保存。可播放查看，或点击「转为 GIF…」。"
		v.useGIFSource(result.Path, float64(result.Status.ElapsedMS)/1000)
	})
}

func (v *videoWindow) pickRecordingRegion(ctx context.Context, source capture.Stream, display mygo.Display, displays []mygo.Display, request recordingRequest) (recordingRegion, error) {
	firstCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	first, err := source.Next(firstCtx)
	cancel()
	if err != nil {
		return recordingRegion{}, err
	}
	if err := waitForCapture(ctx, 200*time.Millisecond); err != nil {
		return recordingRegion{}, err
	}
	screen := displayForStream(source, display, displays)
	size := first.Bounds().Size()
	region, reused := reuseRecordingRegion(request.selection, screen, size)
	if !reused {
		v.later(func() {
			v.notice = "框选要录制的区域，松开即开始。"
			if request.selection != nil {
				v.notice = "共享来源或显示器已变化，请重新框选录屏区域。"
			}
		})
		if screen != nil {
			region, _, err = editor.PickScreenRegion(ctx, first, []mygo.Display{*screen}, request.token, image.Pt(64, 64), "框选录屏区域，松开开始 · Enter 全屏 · Esc 取消")
		} else {
			region, err = editor.SelectVideoRegion(ctx, first)
		}
	} else {
		log.Printf(brand.Name+": recording reused selection: display=%v region=%v", screen.Bounds, region)
	}
	if err != nil {
		return recordingRegion{}, err
	}
	if err := capture.SetStreamRegion(ctx, source, region, size); err != nil {
		return recordingRegion{}, err
	}
	if err := waitForCapture(ctx, 250*time.Millisecond); err != nil {
		return recordingRegion{}, err
	}
	return recordingRegion{screen: screen, region: region, size: size}, nil
}

func (v *videoWindow) recordingStarted(setup recordingSetup, selected recordingRegion) {
	v.app.recordingSetup = setup
	v.running, v.busy, v.paused = true, false, false
	v.path, v.preview, v.status = "", nil, capture.RecordingStatus{}
	v.recoveryPath, v.diagnostic, v.errorDetails = "", "", false
	v.notice = "录制已开始。用选区旁的控制条或托盘暂停、继续或结束。"
	v.app.refreshTray()
	if selected.screen != nil {
		v.openBar(*selected.screen, selected.region, selected.size)
	}
	if v.app.tray == nil && v.win != nil {
		v.win.Show()
		v.notice = "录制中。请把控制窗口移到录制区域外。"
	}
}

func (v *videoWindow) applyRecordingUpdate(update recording.Update) {
	if update.Command != "" {
		v.busy, v.paused = v.ending, update.Command == "pause"
		v.app.refreshTray()
		return
	}
	v.status = update.Status
	if second := update.Status.ElapsedMS / 1000; second != v.traySecond {
		v.traySecond = second
		v.app.refreshTrayClock()
	}
}

func (v *videoWindow) finishRecording(hidden []*mygo.Window) {
	v.app.captureBusy = false
	for _, w := range hidden {
		if !w.IsDestroyed() && w != v.win {
			w.Show()
		}
	}
	v.busy, v.running, v.ending = false, false, false
	v.closeBar()
	v.app.refreshTray()
	if v.quitAfter {
		mygo.App.Quit()
		return
	}
	if !v.closed && v.win != nil {
		v.win.Show()
		v.invalidate()
	}
}
