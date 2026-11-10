package desktop

import (
	"context"
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/components"
	"github.com/wildalley/screenpin/internal/editor"
	"github.com/wildalley/screenpin/internal/recording"
	"golang.org/x/image/bmp"
)

type videoWindow struct {
	preview                                          *ui.Bitmap
	app                                              *application
	win                                              *mygo.Window
	ctx                                              context.Context
	cancel                                           context.CancelFunc
	closed, busy, running, paused, ending, exporting bool
	quitAfter                                        bool
	tab                                              int
	fps, quality                                     string
	notice, path                                     string
	status                                           capture.RecordingStatus
	commands                                         chan string
	gifFPS, gifWidth, gifStart, gifDuration          string
	gifSource, gifResult                             string
	gifLength, gifProgress                           float64
	choosing                                         bool
	exportCancel                                     context.CancelFunc
	traySecond                                       int64
}

const (
	videoTabRecord = 0
	videoTabGIF    = 1
)

func (s *application) RecordWithActivation(string) {
	if s.ctx.Err() != nil || s.settings != nil && s.settings.recording {
		return
	}
	if s.video != nil && s.video.running {
		s.video.command("finish")
		return
	}
	s.showVideo()
}

// recordDir is where recordings and GIFs go without a dialog.
func (s *application) recordDir() string { return s.recordDirFor(s.cfg.RecordDir) }

func (s *application) recordDirFor(configured string) string {
	if configured != "" {
		return configured
	}
	if videos, err := mygo.App.Path(mygo.PathVideos); err == nil && videos != "" {
		return filepath.Join(videos, "ScreenPin")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Videos", "ScreenPin")
}

// chooseRecordDir asks for a folder and saves it as the default at once, so
// the recording window and the settings window agree.
func (s *application) chooseRecordDir(parent *mygo.Window, done func(string)) {
	current := s.recordDir()
	s.worker(func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: "选择录屏保存位置", DefaultPath: current, Directory: true, CreateDirectories: true})
		s.dispatch(func() {
			if err != nil || len(paths) == 0 {
				if err != nil {
					s.notify("选择文件夹失败：" + err.Error())
				}
				return
			}
			next := s.cfg
			next.RecordDir = paths[0]
			if err := next.Save(); err != nil {
				s.notify(err.Error())
				return
			}
			s.cfg.RecordDir = paths[0]
			if s.settings != nil {
				s.settings.draft.RecordDir = paths[0]
				s.settings.win.Invalidate()
			}
			if s.video != nil {
				s.video.win.Invalidate()
			}
			if done != nil {
				done(paths[0])
			}
		})
	})
}

func (s *application) showVideo() { s.openVideo(videoTabRecord) }

// showGIF opens the GIF tab, asking for a video when none is loaded yet.
func (s *application) showGIF() {
	s.openVideo(videoTabGIF)
	if v := s.video; v != nil && !v.running && !v.busy && !v.exporting && v.gifSource == "" {
		v.chooseVideo()
	}
}

func (s *application) openVideo(tab int) {
	if s.video != nil {
		if !s.video.running && !s.video.busy {
			s.video.tab = tab
		}
		s.video.win.Show()
		s.video.win.Focus()
		s.video.win.Invalidate()
		return
	}
	if tab == videoTabRecord && s.captureBusy {
		s.notify("请先完成当前截图或长截图。")
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	v := newVideoWindow(s, ctx, cancel)
	v.tab = tab
	v.win = mygo.NewWindow(mygo.WindowOptions{Title: "ScreenPin 录屏", Width: 480, Height: 500, MinWidth: 400, MinHeight: 360, UseContentSize: true, AlwaysOnTop: true, Content: ui.View(v.view)})
	capture.ExcludeWindow(v.win.NativeHandle())
	s.video = v
	s.operations++
	v.win.OnClose(func(e *mygo.CloseEvent) {
		if v.running || v.busy {
			e.PreventDefault()
			if v.running {
				v.command("finish")
			} else {
				v.cancel()
			}
		}
	})
	v.win.OnClosed(func() {
		v.closed = true
		v.preview = nil
		v.cancel()
		if v.exportCancel != nil {
			v.exportCancel()
		}
		if s.video == v {
			s.video = nil
		}
		s.operations--
		s.refreshTray()
		s.maybeQuit()
	})
}

func newVideoWindow(s *application, ctx context.Context, cancel context.CancelFunc) *videoWindow {
	return &videoWindow{app: s, ctx: ctx, cancel: cancel, fps: "60", quality: "1080p", gifFPS: "15", gifWidth: "960", gifStart: "0", gifDuration: "10", commands: make(chan string, 4), traySecond: -1}
}

func (v *videoWindow) later(fn func()) {
	v.app.dispatch(func() {
		if !v.closed {
			fn()
			v.win.Invalidate()
		}
	})
}

func clock(ms int64) string {
	seconds := ms / 1000
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

// trayStatus describes the recording or export under way for the tray, or
// "" when there is none.
func (v *videoWindow) trayStatus() string {
	switch {
	case v == nil || v.closed:
		return ""
	case v.running && v.ending:
		return "正在保存录屏…"
	case v.running && v.paused:
		return "录屏已暂停 · " + clock(v.status.ElapsedMS)
	case v.running:
		return "● 正在录屏 · " + clock(v.status.ElapsedMS)
	case v.busy:
		return "正在准备录屏：框选区域…"
	case v.exporting:
		return fmt.Sprintf("正在导出 GIF · %d%%", int(v.gifProgress*100))
	}
	return ""
}

func (v *videoWindow) view(c *ui.Context) {
	panelTheme(c)
	if c.Shortcut(0, ui.KeyEscape) && !v.running && !v.busy && !v.exporting {
		v.win.Close()
		return
	}
	locked := v.running || v.busy
	ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "屏幕录制").FontSize(20).FontWeight(600).Grow(1)
			state := "无声 MP4"
			switch {
			case v.ending:
				state = "正在保存…"
			case v.paused:
				state = "已暂停"
			case v.running:
				state = "● 录制中"
			case v.busy:
				state = "准备中"
			case v.exporting:
				state = "导出 GIF 中"
			}
			badge := ui.Badge(c, state)
			if v.running && !v.paused {
				badge.Background(c.Theme().Danger).TextColor(c.Theme().AccentText)
			}
		})
		if !locked {
			previous := v.tab
			ui.Tabs(c, &v.tab, "录制", "转 GIF")
			if previous != v.tab && v.tab == videoTabGIF && v.gifSource == "" && v.path != "" {
				v.useGIFSource(v.path, float64(v.status.ElapsedMS)/1000)
			}
		}
		ui.Scroll(c.Key("video-content")).Grow(1).MinHeight(0).Gap(12).Children(func() {
			if locked || v.tab == videoTabRecord {
				v.recordView(c)
			} else {
				v.gifView(c)
			}
			if v.notice != "" {
				ui.Text(c, v.notice).FontSize(12).Selectable()
			}
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			switch {
			case v.running:
				label := "暂停"
				if v.paused {
					label = "继续"
				}
				if ui.Button(c, label).Disabled(v.busy || v.ending).Clicked() {
					if v.paused {
						v.command("resume")
					} else {
						v.command("pause")
					}
				}
				if ui.PrimaryButton(c, "结束并保存").Disabled(v.busy || v.ending).Clicked() {
					v.command("finish")
				}
			case v.busy:
				if ui.Button(c, "取消").Clicked() {
					v.cancel()
				}
			case v.tab == videoTabGIF:
				if ui.Button(c, "关闭").Disabled(v.exporting).Clicked() {
					v.win.Close()
				}
				if v.exporting {
					if ui.Button(c, "取消导出").Clicked() && v.exportCancel != nil {
						v.exportCancel()
					}
				} else if ui.PrimaryButton(c, "导出 GIF").Disabled(v.gifSource == "" || v.choosing).Clicked() {
					v.exportGIF()
				}
			default:
				if ui.Button(c, "关闭").Disabled(v.exporting).Clicked() {
					v.win.Close()
				}
				if ui.PrimaryButton(c, "选择区域并开始…").Disabled(v.exporting).Clicked() {
					v.start()
				}
			}
		})
	})
}

func (v *videoWindow) recordView(c *ui.Context) {
	muted := c.Theme().TextMuted
	if !v.running && !v.busy {
		ui.Row(c).Gap(12).Children(func() {
			ui.Column(c).Grow(1).Gap(5).Children(func() {
				ui.Text(c, "帧率").FontSize(12)
				ui.Select(c, &v.fps, []string{"30", "60"}).Label("录屏帧率").FillWidth().Disabled(v.exporting)
			})
			ui.Column(c).Grow(1).Gap(5).Children(func() {
				ui.Text(c, "最大分辨率").FontSize(12)
				ui.Select(c, &v.quality, []string{"720p", "1080p", "1440p", "4K"}).Label("录屏分辨率").FillWidth().Disabled(v.exporting)
			})
		})
		ui.Column(c).Gap(5).Children(func() {
			ui.Text(c, "保存位置").FontSize(12)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				where := v.app.recordDir()
				if v.app.cfg.AskRecordPath {
					where = "每次询问（默认 " + where + "）"
				}
				ui.Text(c, where).FontSize(12).MaxLines(2).Grow(1).MinWidth(0).Tooltip(where).Selectable()
				if ui.Button(c.Key("record-dir"), "更改…").Disabled(v.exporting).Clicked() {
					v.app.chooseRecordDir(v.win, nil)
				}
			})
		})
		ui.Text(c, "框选区域后立即开始，结束后自动保存。录制中可从托盘查看时长、暂停或结束。").FontSize(12).TextColor(muted)
	}
	if v.running || v.status.Finished {
		ui.Text(c, clock(v.status.ElapsedMS)).FontSize(32).Font("monospace")
		encoder := v.status.Encoder
		if encoder == "mpeg4" {
			encoder = "MPEG-4 软件编码"
		} else if encoder == "libx264" {
			encoder = "H.264 软件编码"
		} else if encoder != "" {
			encoder = "H.264 硬件编码 · " + encoder
		}
		ui.Text(c, encoder).FontSize(12)
		average := float64(0)
		if v.status.ElapsedMS > 0 {
			average = float64(v.status.Frames) * 1000 / float64(v.status.ElapsedMS)
		}
		ui.Text(c, fmt.Sprintf("%d × %d · 目标 %d fps · 编码平均 %.1f fps", v.status.Width, v.status.Height, v.status.FPS, average)).FontSize(12).TextColor(muted)
		ui.Text(c, fmt.Sprintf("已编码 %d 帧 · 复用静止画面 %d 帧 · 延迟丢帧 %d", v.status.Frames, v.status.Repeated, v.status.Missed)).FontSize(12).TextColor(muted)
	}
	if v.path != "" && !v.running && !v.busy {
		ui.Text(c, "视频已保存").FontWeight(600)
		if v.preview != nil {
			ui.Image(c, v.preview).Fit(ui.Contain).FillWidth().Height(140).Label("视频首帧预览")
		}
		ui.Text(c, v.path).FontSize(12).Selectable()
		ui.Row(c).Wrap().Gap(8).Children(func() {
			if ui.Button(c, "播放视频").Clicked() {
				_ = mygo.Shell.OpenPath(v.path)
			}
			if ui.Button(c, "打开文件夹").Clicked() {
				_ = mygo.Shell.OpenPath(filepath.Dir(v.path))
			}
			if ui.PrimaryButton(c.Key("to-gif"), "转为 GIF…").Disabled(v.exporting).Clicked() {
				v.useGIFSource(v.path, float64(v.status.ElapsedMS)/1000)
				v.tab = videoTabGIF
			}
		})
	}
}

func (v *videoWindow) gifView(c *ui.Context) {
	muted := c.Theme().TextMuted
	ui.Column(c).Gap(5).Children(func() {
		ui.Text(c, "视频").FontSize(12)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			name := "尚未选择视频"
			if v.gifSource != "" {
				name = filepath.Base(v.gifSource)
				if v.gifLength > 0 {
					name += " · " + clock(int64(v.gifLength*1000))
				}
			}
			ui.Text(c, name).MaxLines(2).Grow(1).MinWidth(0).Tooltip(v.gifSource)
			label := "选择视频…"
			if v.choosing {
				label = "正在打开…"
			}
			if ui.Button(c.Key("gif-source"), label).Disabled(v.exporting || v.choosing).Clicked() {
				v.chooseVideo()
			}
		})
	})
	if v.gifSource == "" {
		ui.Text(c, "选择一段 MP4 视频，或先在「录制」页录一段。").FontSize(12).TextColor(muted)
		return
	}
	ui.Row(c).Gap(8).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			ui.Text(c, "开始（秒）").FontSize(12)
			ui.TextInput(c, &v.gifStart).Label("GIF 开始秒数").Placeholder("0").FillWidth().Disabled(v.exporting)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			ui.Text(c, "时长（秒，最多 60）").FontSize(12)
			ui.TextInput(c, &v.gifDuration).Label("GIF 时长秒数").Placeholder("10").FillWidth().Disabled(v.exporting)
		})
	})
	ui.Row(c).Gap(8).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			ui.Text(c, "帧率").FontSize(12)
			ui.Select(c, &v.gifFPS, []string{"10", "15", "20", "30"}).Label("GIF 帧率").FillWidth().Disabled(v.exporting)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			ui.Text(c, "最大宽度").FontSize(12)
			ui.Select(c, &v.gifWidth, []string{"640", "960", "1280", "1920"}).Label("GIF 宽度").FillWidth().Disabled(v.exporting)
		})
	})
	where := "保存在视频旁边"
	if v.app.cfg.AskRecordPath {
		where = "导出时选择保存位置"
	}
	ui.Text(c, where+"。较低的帧率和宽度文件更小，更适合分享。").FontSize(12).TextColor(muted)
	if v.exporting {
		ui.Progress(c, v.gifProgress).FillWidth().Label("GIF 导出进度")
		ui.Text(c, fmt.Sprintf("正在导出 %d%%", int(v.gifProgress*100))).FontSize(12).TextColor(muted)
	}
	if v.gifResult != "" && !v.exporting {
		ui.Text(c, "GIF 已保存").FontWeight(600)
		ui.Text(c, v.gifResult).FontSize(12).Selectable()
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c.Key("open-gif"), "打开 GIF").Clicked() {
				_ = mygo.Shell.OpenPath(v.gifResult)
			}
			if ui.Button(c.Key("open-gif-folder"), "打开文件夹").Clicked() {
				_ = mygo.Shell.OpenPath(filepath.Dir(v.gifResult))
			}
		})
	}
}

// useGIFSource loads a video into the GIF tab; length 0 means unknown.
func (v *videoWindow) useGIFSource(path string, length float64) {
	v.gifSource, v.gifLength, v.gifResult = path, length, ""
	v.gifStart = "0"
	duration := 10.0
	if length > 0 {
		duration = min(10, length)
	}
	v.gifDuration = strconv.FormatFloat(duration, 'f', -1, 64)
	if length > 0 && duration != float64(int(duration)) {
		v.gifDuration = strconv.FormatFloat(duration, 'f', 2, 64)
	}
}

func (v *videoWindow) chooseVideo() {
	if v.choosing || v.exporting {
		return
	}
	v.choosing = true
	dir := v.app.recordDir()
	if v.gifSource != "" {
		dir = filepath.Dir(v.gifSource)
	}
	v.app.worker(func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: v.win, Title: "选择要转为 GIF 的视频", DefaultPath: dir, Filters: []mygo.FileFilter{{Name: "MP4 视频", Extensions: []string{"mp4", "m4v", "mov"}}}})
		var length float64
		if err == nil && len(paths) > 0 {
			length, _ = recording.Duration(paths[0])
		}
		v.later(func() {
			v.choosing = false
			if err != nil {
				v.notice = "打开视频失败：" + err.Error()
				return
			}
			if len(paths) > 0 {
				v.useGIFSource(paths[0], length)
				v.notice = ""
				if length == 0 {
					v.notice = "无法读取视频时长，请确认起点不超过视频长度。"
				}
			}
		})
	})
}

func (v *videoWindow) command(command string) {
	if !v.running || v.ending || (v.busy && command != "finish") {
		return
	}
	select {
	case v.commands <- command:
		v.busy = true
		if command == "finish" {
			v.ending = true
		}
		v.win.Invalidate()
	default:
	}
}
func (v *videoWindow) start() {
	if v.busy || v.running || v.app.captureBusy {
		return
	}
	if _, err := components.Find("screenpin-record"); err != nil {
		v.notice = err.Error()
		return
	}
	if v.ctx.Err() != nil {
		v.ctx, v.cancel = context.WithCancel(v.app.ctx)
	}
	v.commands = make(chan string, 4)
	ctx := v.ctx
	fps, _ := strconv.Atoi(v.fps)
	edge := map[string]int{"720p": 1280, "1080p": 1920, "1440p": 2560, "4K": 3840}[v.quality]
	v.busy = true
	v.notice = "框选要录制的区域，松开即开始。"
	ask, dir := v.app.cfg.AskRecordPath, v.app.recordDir()
	v.app.captureBusy = true
	v.app.refreshTray()
	displays := mygo.Screen.Displays()
	display := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	v.app.worker(func() {
		var hidden []*mygo.Window
		defer v.app.dispatch(func() {
			// Release ownership and restore windows even if the control was destroyed.
			v.app.captureBusy = false
			for _, w := range hidden {
				if !w.IsDestroyed() && w != v.win {
					w.Show()
				}
			}
			v.busy, v.running, v.ending = false, false, false
			v.app.refreshTray()
			if v.quitAfter {
				mygo.App.Quit()
				return
			}
			if !v.closed {
				v.win.Show()
				v.win.Invalidate()
			}
		})
		path, err := recording.NextPath(dir, "录屏", ".mp4", time.Now())
		if ask {
			if err == nil {
				path, err = mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: v.win, Title: "保存录屏视频", DefaultPath: path, Filters: []mygo.FileFilter{{Name: "MP4 视频", Extensions: []string{"mp4"}}}})
			}
			if err == nil && path == "" {
				v.later(func() { v.notice = "" })
				return
			}
			if err == nil && !strings.EqualFold(filepath.Ext(path), ".mp4") {
				path += ".mp4"
			}
		}
		if err != nil {
			v.report(err)
			return
		}
		staged, err := recording.Stage(path)
		if err != nil {
			v.report(err)
			return
		}
		keep := false
		defer func() {
			info, statErr := os.Stat(staged)
			if !keep || statErr == nil && info.Size() == 0 {
				_ = os.Remove(staged)
			}
		}()
		mygo.RunOnMain(func() {
			displays = mygo.Screen.Displays()
			display = mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
			for _, w := range mygo.Windows() {
				if w.IsVisible() {
					hidden = append(hidden, w)
					w.Hide()
				}
			}
		})
		options := capture.StreamOptions{Recording: &capture.RecordingOptions{Path: staged, FPS: fps, MaxEdge: edge}}
		monitors, err := capture.StreamMonitors(ctx)
		if err != nil {
			v.report(err)
			return
		}
		for _, m := range monitors {
			if m.UnderMouse {
				options.Monitor = m.ID
				break
			}
		}
		if runtime.GOOS == "darwin" {
			options.Monitor = int(display.ID)
		}
		source, err := capture.OpenStream(ctx, options)
		if err != nil {
			v.report(err)
			return
		}
		defer source.Close()
		firstCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		first, err := source.Next(firstCtx)
		cancel()
		if err != nil {
			v.report(err)
			return
		}
		time.Sleep(200 * time.Millisecond)
		var screen *mygo.Display
		if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
			screen = &display
		} else if bounds, ok := capture.StreamDisplayBounds(source); ok {
			for _, d := range displays {
				if d.Bounds.X == bounds.Min.X && d.Bounds.Y == bounds.Min.Y && d.Bounds.Width == bounds.Dx() && d.Bounds.Height == bounds.Dy() {
					copy := d
					screen = &copy
					break
				}
			}
		}
		if screen == nil && capture.StreamSourceType(source) == 1 && len(displays) == 1 {
			screen = &displays[0]
		}
		var region image.Rectangle
		if screen != nil {
			region, _, err = editor.PickScreenRegion(ctx, first, []mygo.Display{*screen}, "", image.Pt(64, 64), "框选录屏区域，松开开始 · Enter 全屏 · Esc 取消")
		} else {
			region, err = editor.SelectVideoRegion(ctx, first)
		}
		size := first.Bounds().Size()
		first = nil
		if err == nil {
			err = capture.SetStreamRegion(ctx, source, region, size)
		}
		if err == nil {
			time.Sleep(250 * time.Millisecond)
			keep = true
			err = capture.RecordingCommand(ctx, source, "record")
		}
		if err != nil {
			v.report(err)
			return
		}
		keep = true // Once recording begins, interrupted video must remain recoverable.
		v.later(func() {
			v.running = true
			v.busy = false
			v.paused = false
			v.path = ""
			v.preview = nil
			v.status = capture.RecordingStatus{}
			v.notice = "录制已开始，控制窗口已隐藏。托盘显示录制时长，可暂停、继续或结束。"
			v.app.refreshTray()
			if v.app.tray == nil {
				v.win.Show()
				v.notice = "录制中。请把控制窗口移到录制区域外。"
			}
		})
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				v.report(fmt.Errorf("录屏已中断，视频片段保留于 %s", staged))
				return
			case command := <-v.commands:
				err = capture.RecordingCommand(ctx, source, command)
				if err != nil {
					keep = true
					v.report(fmt.Errorf("录屏中断，视频片段保留于 %s\n%w", staged, err))
					return
				}
				if command == "finish" {
					// Native workers send final status before acknowledging finish.
					status, snapshotErr := capture.RecordingSnapshot(source)
					if snapshotErr != nil || !status.Finished {
						v.report(fmt.Errorf("未收到视频保存确认，片段保留于 %s", staged))
						return
					}
					source.Close()
					if err = recording.Publish(staged, path); err != nil {
						keep = true
						v.report(fmt.Errorf("保存失败，视频保留于 %s\n%w", staged, err))
						return
					}
					preview := videoPreview(ctx, path)
					v.later(func() {
						if preview != nil {
							v.preview = ui.NewBitmap(preview)
						}
						v.status = status
						v.path = path
						v.notice = "视频已保存。可播放查看，或点击「转为 GIF…」。"
						v.useGIFSource(path, float64(status.ElapsedMS)/1000)
					})
					return
				}
				v.later(func() { v.busy = v.ending; v.paused = command == "pause"; v.app.refreshTray() })
			case <-ticker.C:
				status, e := capture.RecordingSnapshot(source)
				if e != nil {
					keep = true
					v.report(fmt.Errorf("采集已结束，视频片段保留于 %s\n%w", staged, e))
					return
				}
				v.later(func() {
					v.status = status
					// The tray shows elapsed seconds; rebuild its menu only when they change.
					if second := status.ElapsedMS / 1000; second != v.traySecond {
						v.traySecond = second
						v.app.refreshTray()
					}
				})
			}
		}
	})
}
func (v *videoWindow) exportGIF() {
	if v.exporting || v.gifSource == "" {
		return
	}
	start, e1 := strconv.ParseFloat(strings.TrimSpace(v.gifStart), 64)
	duration, e2 := strconv.ParseFloat(strings.TrimSpace(v.gifDuration), 64)
	fps, _ := strconv.Atoi(v.gifFPS)
	width, _ := strconv.Atoi(v.gifWidth)
	if e1 != nil || e2 != nil {
		v.notice = "请输入有效的开始秒数和时长。"
		return
	}
	if v.gifLength > 0 {
		if start >= v.gifLength {
			v.notice = fmt.Sprintf("开始时间超出视频长度（%.1f 秒）。", v.gifLength)
			return
		}
		duration = min(duration, v.gifLength-start)
	}
	o := recording.GIFOptions{Start: start, Duration: duration, FPS: fps, Width: width}
	if err := o.Validate(); err != nil {
		v.notice = err.Error()
		return
	}
	ctx, cancel := context.WithCancel(v.ctx)
	v.exportCancel = cancel
	v.exporting, v.gifProgress, v.gifResult = true, 0, ""
	v.notice = ""
	v.app.refreshTray()
	source, ask := v.gifSource, v.app.cfg.AskRecordPath
	shown := -1
	o.Progress = func(done float64) {
		// Called on the export goroutine; forward whole percents only.
		if percent := int(done * 100); percent != shown {
			shown = percent
			v.later(func() {
				v.gifProgress = done
				v.app.refreshTray()
			})
		}
	}
	v.app.worker(func() {
		defer cancel()
		path := recording.SiblingPath(source, ".gif")
		var err error
		if ask {
			path, err = mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: v.win, Title: "导出 GIF", DefaultPath: path, Filters: []mygo.FileFilter{{Name: "GIF 动图", Extensions: []string{"gif"}}}})
			if err == nil && path != "" && !strings.EqualFold(filepath.Ext(path), ".gif") {
				path += ".gif"
			}
		}
		if err == nil && path != "" {
			err = recording.ExportGIF(ctx, source, path, o)
		}
		v.later(func() {
			v.exporting = false
			v.exportCancel = nil
			v.app.refreshTray()
			if v.quitAfter {
				mygo.App.Quit()
				return
			}
			switch {
			case err != nil && ctx.Err() != nil:
				v.notice = "已取消导出，原视频保留。"
			case err != nil:
				v.notice = err.Error()
			case path != "":
				v.gifResult = path
			}
		})
	})
}

// Copy errors before dispatch: worker locals may change before the UI executes.
func (v *videoWindow) report(err error) {
	message := err.Error()
	log.Print("recording: ", message)
	v.later(func() {
		v.notice = message
		if v.running {
			v.quitAfter = false
		}
	})
}

func videoPreview(ctx context.Context, path string) image.Image {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "screenpin-video-preview-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "preview.bmp")
	if recording.Thumbnail(ctx, path, file) != nil {
		return nil
	}
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _ := bmp.Decode(f)
	return img
}
