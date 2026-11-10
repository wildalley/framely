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
	fps, quality                                     string
	notice, path                                     string
	status                                           capture.RecordingStatus
	commands                                         chan string
	gifFPS, gifWidth, gifStart, gifDuration          string
	exportCancel                                     context.CancelFunc
}

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
func (s *application) showVideo() {
	if s.video != nil {
		s.video.win.Show()
		s.video.win.Focus()
		return
	}
	if s.captureBusy {
		s.notify("请先完成当前截图或长截图。")
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	v := &videoWindow{app: s, ctx: ctx, cancel: cancel, fps: "60", quality: "1080p", gifFPS: "15", gifWidth: "960", gifStart: "0", gifDuration: "10", commands: make(chan string, 4)}
	v.win = mygo.NewWindow(mygo.WindowOptions{Title: "ScreenPin 录屏", Width: 480, Height: 460, MinWidth: 400, MinHeight: 340, UseContentSize: true, AlwaysOnTop: true, Content: ui.View(v.view)})
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
		s.maybeQuit()
	})
}
func (v *videoWindow) later(fn func()) {
	v.app.dispatch(func() {
		if !v.closed {
			fn()
			v.win.Invalidate()
		}
	})
}
func (v *videoWindow) view(c *ui.Context) {
	panelTheme(c)
	if c.Shortcut(0, ui.KeyEscape) && !v.running && !v.busy && !v.exporting {
		v.win.Close()
		return
	}
	ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "屏幕录制").FontSize(20).FontWeight(600).Grow(1)
			state := "无声视频"
			if v.running {
				state = "录制中"
			}
			if v.paused {
				state = "已暂停"
			}
			if v.ending {
				state = "正在完成…"
			}
			ui.Text(c, state).FontSize(12).TextColor(c.Theme().TextMuted)
		})
		ui.Scroll(c.Key("video-content")).Grow(1).MinHeight(0).Gap(12).Children(func() {
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
				ui.Text(c, "框选区域后开始，优先使用硬件编码。实际流畅度取决于设备和共享来源。").FontSize(12).TextColor(c.Theme().TextMuted)
				ui.Text(c, "可从托盘暂停、继续或结束录屏。").FontSize(12).TextColor(c.Theme().TextMuted)
			}
			if v.running || v.status.Finished {
				seconds := v.status.ElapsedMS / 1000
				ui.Text(c, fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)).FontSize(32).Font("monospace")
				encoder := v.status.Encoder
				if encoder == "mpeg4" {
					encoder = "MPEG-4 软件编码"
				} else if encoder == "libx264" {
					encoder = "H.264 软件编码"
				} else if encoder != "" {
					encoder = "H.264 硬件编码 · " + encoder
				}
				ui.Text(c, encoder).FontSize(12)
				ui.Text(c, fmt.Sprintf("收到画面 %d 帧 · 复用静止画面 %d 帧", v.status.Captured, v.status.Repeated)).FontSize(12).TextColor(c.Theme().TextMuted)
				average := float64(0)
				if v.status.ElapsedMS > 0 {
					average = float64(v.status.Frames) * 1000 / float64(v.status.ElapsedMS)
				}
				ui.Text(c, fmt.Sprintf("%d × %d · 目标 %d fps · 编码平均 %.1f fps", v.status.Width, v.status.Height, v.status.FPS, average)).FontSize(12).TextColor(c.Theme().TextMuted)
				ui.Text(c, fmt.Sprintf("已编码 %d 帧 · 延迟丢帧 %d", v.status.Frames, v.status.Missed)).FontSize(12).TextColor(c.Theme().TextMuted)
			}
			if v.path != "" && !v.running && !v.busy {
				ui.Text(c, "视频已保存").FontWeight(600)
				if v.preview != nil {
					ui.Image(c, v.preview).Fit(ui.Contain).FillWidth().Height(140).Label("视频首帧预览")
				}
				ui.Text(c, v.path).FontSize(12).Selectable()
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, "播放视频").Clicked() {
						_ = mygo.Shell.OpenPath(v.path)
					}
					if ui.Button(c, "打开文件夹").Clicked() {
						_ = mygo.Shell.OpenPath(filepath.Dir(v.path))
					}
				})
				ui.Text(c, "导出 GIF").FontWeight(600)
				ui.Row(c).Gap(8).Children(func() {
					ui.TextInput(c, &v.gifStart).Label("GIF 开始秒数").Placeholder("开始秒数").Grow(1).MinWidth(0).Disabled(v.exporting)
					ui.TextInput(c, &v.gifDuration).Label("GIF 时长秒数").Placeholder("时长秒数").Grow(1).MinWidth(0).Disabled(v.exporting)
				})
				ui.Text(c, "起点 / 时长（秒），单次最多 60 秒。").FontSize(12).TextColor(c.Theme().TextMuted)
				ui.Row(c).Gap(8).Children(func() {
					ui.Select(c, &v.gifFPS, []string{"10", "15", "20", "30"}).Label("GIF 帧率").Grow(1).Disabled(v.exporting)
					ui.Select(c, &v.gifWidth, []string{"640", "960", "1280", "1920"}).Label("GIF 宽度").Grow(1).Disabled(v.exporting)
				})
				ui.Text(c, "GIF 帧率 / 最大宽度，较低设置更适合分享。").FontSize(12).TextColor(c.Theme().TextMuted)
				if v.exporting {
					if ui.Button(c, "取消导出").Clicked() && v.exportCancel != nil {
						v.exportCancel()
					}
				} else if ui.Button(c, "导出 GIF…").Clicked() {
					v.exportGIF()
				}
			}
			if v.notice != "" {
				ui.Text(c, v.notice).FontSize(12).Selectable()
			}
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if v.running {
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
			} else if v.busy {
				if ui.Button(c, "取消").Clicked() {
					v.cancel()
				}
			} else {
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
	v.notice = "先选择视频保存位置，再框选录制区域。"
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
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: v.win, Title: "保存录屏视频", DefaultPath: "录屏 " + time.Now().Format("2006-01-02 15-04-05") + ".mp4", Filters: []mygo.FileFilter{{Name: "MP4 视频", Extensions: []string{"mp4"}}}})
		if err != nil || path == "" {
			if err != nil {
				v.report(err)
			}
			return
		}
		if !strings.EqualFold(filepath.Ext(path), ".mp4") {
			path += ".mp4"
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
			v.notice = "录制已开始，控制窗口已隐藏。通过托盘暂停、继续或结束录屏。"
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
						v.notice = "视频已保存。可播放查看，或导出 GIF。"
						seconds := float64(status.ElapsedMS) / 1000
						v.gifDuration = strconv.FormatFloat(min(10, seconds), 'f', 2, 64)
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
				v.later(func() { v.status = status })
			}
		}
	})
}
func (v *videoWindow) exportGIF() {
	start, e1 := strconv.ParseFloat(v.gifStart, 64)
	duration, e2 := strconv.ParseFloat(v.gifDuration, 64)
	fps, _ := strconv.Atoi(v.gifFPS)
	width, _ := strconv.Atoi(v.gifWidth)
	o := recording.GIFOptions{Start: start, Duration: duration, FPS: fps, Width: width}
	if e1 != nil || e2 != nil {
		v.notice = "请输入有效的开始秒数和时长。"
		return
	}
	if err := o.Validate(); err != nil {
		v.notice = err.Error()
		return
	}
	if start >= float64(v.status.ElapsedMS)/1000 {
		v.notice = "开始时间超出视频长度。"
		return
	}
	ctx, cancel := context.WithCancel(v.ctx)
	v.exportCancel = cancel
	v.exporting = true
	source := v.path
	v.app.worker(func() {
		defer cancel()
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: v.win, Title: "导出 GIF", DefaultPath: strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)) + ".gif", Filters: []mygo.FileFilter{{Name: "GIF 动图", Extensions: []string{"gif"}}}})
		if err == nil && path != "" {
			if !strings.EqualFold(filepath.Ext(path), ".gif") {
				path += ".gif"
			}
			v.later(func() { v.notice = "正在生成调色板并导出 GIF…" })
			err = recording.ExportGIF(ctx, source, path, o)
		}
		v.later(func() {
			v.exporting = false
			v.exportCancel = nil
			if v.quitAfter {
				mygo.App.Quit()
				return
			}
			if err != nil {
				if ctx.Err() != nil {
					v.notice = "已取消导出，原视频保留。"
				} else {
					v.notice = err.Error()
				}
			} else if path != "" {
				v.notice = "GIF 已保存：" + path
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
