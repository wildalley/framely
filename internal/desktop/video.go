package desktop

import (
	"context"
	"errors"
	"fmt"
	"github.com/wildalley/framely/internal/brand"
	"image"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/capture"
	"github.com/wildalley/framely/internal/components"
	"github.com/wildalley/framely/internal/editor"
	"github.com/wildalley/framely/internal/recording"
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
	recoveryPath, diagnostic                         string
	errorDetails                                     bool
	openPath                                         func(string) error
	status                                           capture.RecordingStatus
	commands                                         chan string
	gifFPS, gifWidth, gifStart, gifDuration          string
	gifSource, gifResult                             string
	gifLength, gifProgress                           float64
	gifLow, gifHigh                                  float64
	gifErrors                                        map[string]string
	gifFrame                                         gifFramePreview
	choosing                                         bool
	exportCancel                                     context.CancelFunc
	traySecond                                       int64
	// bar is the floating control beside the recorded region, and frame is the
	// click-through selection border outside the recorded pixels.
	bar, frame *mygo.Window
	frameHole  ui.Rect
}

const (
	videoTabRecord = 0
	videoTabGIF    = 1
)

func (s *application) RecordWithActivation(token string) {
	if s.ctx.Err() != nil || s.settings != nil && s.settings.recording {
		return
	}
	if s.video != nil && s.video.running {
		s.video.command("finish")
		return
	}
	if s.captureBusy || s.video != nil && (s.video.busy || s.video.exporting) {
		return
	}
	s.openVideo(videoTabRecord)
	if s.video != nil {
		s.video.startRecording(true, token, nil)
	}
}

// showVideoForSelection reports whether the screenshot was handed to a new
// recording, so only that accepted handoff closes the screenshot window.
func (s *application) showVideoForSelection(selection *editor.ScreenSelection) bool {
	if selection == nil {
		s.showVideo()
		return false
	}
	if s.captureBusy || s.video != nil && (s.video.busy || s.video.running || s.video.exporting) {
		return false
	}
	s.openVideo(videoTabRecord)
	if s.video != nil {
		s.video.startRecording(true, "", selection)
		return s.video.busy
	}
	return false
}

// recordDir is where recordings and GIFs go without a dialog.
func (s *application) recordDir() string { return s.recordDirFor(s.cfg.RecordDir) }

func (s *application) recordDirFor(configured string) string {
	if configured != "" {
		return configured
	}
	if videos, err := mygo.App.Path(mygo.PathVideos); err == nil && videos != "" {
		return filepath.Join(videos, brand.Name)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Videos", brand.Name)
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
	v.gifFrame.load, v.gifFrame.run, v.gifFrame.later = videoPreviewAt, s.worker, v.later
	v.tab = tab
	v.win = mygo.NewWindow(mygo.WindowOptions{Title: brand.Title("录屏"), Width: 480, Height: 500, MinWidth: 400, MinHeight: 360, UseContentSize: true, AlwaysOnTop: true, Content: ui.View(v.view)})
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
		v.cancelGIFPreview()
		v.gifFrame.bitmap = nil
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
	setup := s.recordingSetup.normalized()
	return &videoWindow{app: s, ctx: ctx, cancel: cancel, fps: setup.FPS, quality: setup.Quality, gifFPS: "15", gifWidth: "960", gifStart: "0", gifDuration: "10", commands: make(chan string, 4), traySecond: -1}
}

func (v *videoWindow) later(fn func()) {
	v.app.dispatch(func() {
		if !v.closed {
			fn()
			v.invalidate()
		}
	})
}

func (v *videoWindow) invalidate() {
	for _, window := range []*mygo.Window{v.win, v.bar, v.frame} {
		if window != nil && !window.IsDestroyed() {
			window.Invalidate()
		}
	}
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
		return "正在准备录屏…"
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
		panelHeader(c, "屏幕录制", "区域录屏 · 无声 MP4 · 可转 GIF", func() {
			state := "就绪"
			switch {
			case v.ending:
				state = "正在保存…"
			case v.paused:
				state = "已暂停"
			case v.running:
				state = "录制中"
			case v.busy:
				state = "准备中"
			case v.exporting:
				state = "导出 GIF 中"
			}
			badge := ui.Badge(c, state).Shrink(0)
			if v.running && !v.paused {
				badge.Background(brand.Signal).TextColor(c.Theme().AccentText)
			}
		})
		if !locked {
			previous := v.tab
			ui.Tabs(c, &v.tab, "录制", "转 GIF")
			if previous != v.tab && v.tab == videoTabGIF && v.gifSource == "" && v.path != "" {
				v.useGIFSource(v.path, float64(v.status.ElapsedMS)/1000)
			} else if previous != v.tab {
				if v.tab == videoTabGIF {
					v.requestGIFPreview(v.gifLow)
				} else {
					v.cancelGIFPreview()
				}
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
			if v.recoveryPath != "" {
				ui.Text(c, v.recoveryPath).FontSize(12).MaxLines(2).Tooltip(v.recoveryPath).Selectable()
				ui.Row(c).Wrap().Gap(8).Children(func() {
					if ui.Button(c.Key("record-recovery-open"), "打开保留片段").Clicked() {
						v.openMediaPath(v.recoveryPath)
					}
					if ui.Button(c.Key("record-recovery-folder"), "打开片段文件夹").Clicked() {
						v.openMediaPath(filepath.Dir(v.recoveryPath))
					}
				})
			}
			if v.diagnostic != "" {
				ui.Row(c).Wrap().Gap(8).Children(func() {
					label := "查看错误详情"
					if v.errorDetails {
						label = "收起错误详情"
					}
					if ui.Button(c.Key("video-error-details"), label).Clicked() {
						v.errorDetails = !v.errorDetails
					}
					if v.errorDetails && ui.Button(c.Key("video-copy-error"), "复制错误详情").Clicked() {
						mygo.Clipboard.WriteText(v.diagnostic)
						c.Toast("已复制错误详情")
					}
				})
				if v.errorDetails {
					ui.Text(c, v.diagnostic).FontSize(12).MaxLines(4).Tooltip(v.diagnostic).Selectable()
				}
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
				label := "选择区域并开始…"
				if v.app.recordingSetup.Selection != nil {
					label = "录制上次区域"
					if ui.Button(c, "重新选区…").Disabled(v.exporting).Clicked() {
						v.startRecording(false, "", nil)
					}
				}
				if ui.PrimaryButton(c, label).Disabled(v.exporting).Clicked() {
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
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(8, 10).Radius(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
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
		if selection := v.app.recordingSetup.Selection; selection != nil {
			ui.Text(c, fmt.Sprintf("上次区域 %d × %d · 将沿用同一显示器上的位置", selection.Region.Dx(), selection.Region.Dy())).FontSize(12).TextColor(muted)
		}
		ui.Text(c, "首次框选后自动记住区域，再次录制无需重选。结束后自动保存，也可从托盘暂停或结束。").FontSize(12).TextColor(muted)
	}
	if v.running || v.status.Finished {
		ui.Column(c).FillWidth().Padding(10, 12).Gap(4).Radius(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
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
		})
	}
	if v.path != "" && !v.running && !v.busy {
		ui.Column(c).FillWidth().Padding(10, 12).Gap(8).Radius(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
			ui.Text(c, "视频已保存").FontWeight(600)
			if v.preview != nil {
				ui.Image(c, v.preview).Fit(ui.Contain).FillWidth().Height(140).Label("视频首帧预览")
			}
			ui.Text(c, v.path).FontSize(12).Selectable()
			ui.Row(c).Wrap().Gap(8).Children(func() {
				if ui.Button(c, "播放视频").Clicked() {
					v.openMediaPath(v.path)
				}
				if ui.Button(c, "打开文件夹").Clicked() {
					v.openMediaPath(filepath.Dir(v.path))
				}
				if ui.PrimaryButton(c.Key("to-gif"), "转为 GIF…").Disabled(v.exporting).Clicked() {
					v.useGIFSource(v.path, float64(v.status.ElapsedMS)/1000)
					v.tab = videoTabGIF
				}
			})
		})
	}
}

func (v *videoWindow) gifView(c *ui.Context) {
	muted := c.Theme().TextMuted
	ui.Column(c).Gap(5).Children(func() {
		ui.Text(c, "视频").FontSize(12)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(8, 10).Radius(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
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
	v.gifSelectionView(c)
	ui.Row(c).Gap(8).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			ui.Text(c, "开始（秒）").FontSize(12)
			if ui.TextInput(c, &v.gifStart).Label("GIF 开始秒数").Placeholder("0").FillWidth().Disabled(v.exporting).Invalid(v.gifErrors["start"] != "").Changed() {
				v.syncGIFRange(true)
			}
			v.gifFieldError(c, "start")
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			ui.Text(c, "时长（秒，最多 60）").FontSize(12)
			if ui.TextInput(c, &v.gifDuration).Label("GIF 时长秒数").Placeholder("10").FillWidth().Disabled(v.exporting).Invalid(v.gifErrors["duration"] != "").Changed() {
				v.syncGIFRange(false)
			}
			v.gifFieldError(c, "duration")
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
		ui.Column(c).FillWidth().Padding(10, 12).Gap(8).Radius(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
			ui.Text(c, "GIF 已保存").FontWeight(600)
			ui.Text(c, v.gifResult).FontSize(12).Selectable()
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c.Key("open-gif"), "打开 GIF").Clicked() {
					v.openMediaPath(v.gifResult)
				}
				if ui.Button(c.Key("open-gif-folder"), "打开文件夹").Clicked() {
					v.openMediaPath(filepath.Dir(v.gifResult))
				}
			})
		})
	}
}

// useGIFSource loads a video into the GIF tab; length 0 means unknown.
func (v *videoWindow) useGIFSource(path string, length float64) {
	if math.IsNaN(length) || math.IsInf(length, 0) || length < 0 {
		length = 0
	}
	v.cancelGIFPreview()
	v.gifFrame.bitmap, v.gifFrame.err = nil, ""
	v.gifErrors = nil
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
	v.gifLow, v.gifHigh = 0, duration
	v.requestGIFPreview(0)
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
		var durationErr error
		if err == nil && len(paths) > 0 {
			length, durationErr = recording.DurationContext(v.ctx, paths[0])
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
				if durationErr != nil {
					log.Printf(brand.Name+": video duration: %v", durationErr)
					v.notice = "无法读取视频时长，请确认起点不超过视频长度。"
				}
			}
		})
	})
}

const recordBarW, recordBarH = 304, 56

var (
	recordPauseIcon  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5v14M15 5v14"/></svg>`))
	recordResumeIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m8 5 11 7-11 7z"/></svg>`))
	recordStopIcon   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>`))
)

// openBar shows timer, pause and stop beside the recorded region, so the
// recording can be controlled without the tray.
func (v *videoWindow) openBar(display mygo.Display, region image.Rectangle, source image.Point) {
	if v.bar != nil || source.X <= 0 || source.Y <= 0 {
		return
	}
	b := display.Bounds
	sx, sy := float32(b.Width)/float32(source.X), float32(b.Height)/float32(source.Y)
	selected := ui.Rect{X: float32(region.Min.X) * sx, Y: float32(region.Min.Y) * sy, W: float32(region.Dx()) * sx, H: float32(region.Dy()) * sy}
	if v.frame == nil {
		if fw, _, hole, ok := openRegionFrame(v.ctx, brand.Title("录屏选区"), display, selected, v.frameView, v.app.worker); ok {
			v.frame, v.frameHole = fw, hole
		}
	}
	rect, ok := barBeside(selected, float32(b.Width), float32(b.Height), recordBarW, recordBarH)
	if !ok {
		v.notice = "录制区域周围没有空间放置控制条，请用托盘或录屏快捷键结束。"
		return
	}
	w, err := openControlBar(v.ctx, brand.Title("录屏控制"), display, rect, v.barView, v.app.worker)
	if err != nil {
		v.notice = err.Error() + "，请用托盘或录屏快捷键结束。"
		return
	}
	v.bar = w
	// Closing the bar (for example with the compositor's close key) ends the
	// recording and saves it, as the main window's close does.
	w.OnClose(func(e *mygo.CloseEvent) {
		if v.running {
			e.PreventDefault()
			v.command("finish")
		}
	})
}

func (v *videoWindow) closeBar() {
	if v.bar != nil {
		if !v.bar.IsDestroyed() {
			v.bar.Destroy()
		}
		v.bar = nil
	}
	if v.frame != nil {
		if !v.frame.IsDestroyed() {
			v.frame.Destroy()
		}
		v.frame = nil
	}
}

func (v *videoWindow) frameView(c *ui.Context) {
	panelTheme(c)
	t := c.Theme()
	stroke := brand.Signal
	if v.paused {
		stroke = t.TextMuted
	}
	if v.ending {
		stroke = brand.Accent(t.Dark)
	}
	drawRegionFrame(c, v.frameHole, stroke)
}

func (v *videoWindow) barView(c *ui.Context) {
	panelTheme(c)
	c.Root().Background(ui.Transparent)
	t := c.Theme()
	ui.Row(c).Fill().Padding(8).Gap(6).AlignItems(ui.Center).Background(t.Background).
		Border(1, t.Border).Radius(12).Children(func() {
		state, dot := "录制中", brand.Signal
		if v.paused {
			state, dot = "已暂停", t.TextMuted
		}
		if v.ending {
			state, dot = "正在保存…", brand.Accent(t.Dark)
		}
		// Keep the timer and both actions in fixed slots, including hour-long
		// recordings and in-flight commands, so a tick never moves the buttons.
		ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Padding(0, 4).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
			ui.Column(c).Gap(1).Children(func() {
				ui.Text(c, state).FontSize(11).TextColor(t.TextMuted).SingleLine()
				ui.Text(c, clock(v.status.ElapsedMS)).Font("monospace").FontSize(16).FontWeight(600).SingleLine()
			})
		})
		ui.Box(c).Size(1, 24).Background(t.Border).Shrink(0)
		pause, icon := "暂停", recordPauseIcon
		if v.paused {
			pause, icon = "继续", recordResumeIcon
		}
		button := ui.Button(c.Key("bar-pause"), "").Size(72, 36).Padding(0).Gap(5).Radius(8).Shrink(0).
			Border(0, ui.Transparent).Label(pause).
			Disabled(!v.running || v.busy || v.ending)
		background := t.Surface
		if button.Pressed() {
			background = t.SurfacePressed
		} else if button.Hovered() {
			background = t.SurfaceHover
		}
		button.Background(background).Children(func() {
			ui.Icon(c, icon).Size(16, 16).AccessHidden(true)
			ui.Text(c, pause).FontSize(13).SingleLine()
		})
		if button.Clicked() {
			if v.paused {
				v.command("resume")
			} else {
				v.command("pause")
			}
		}
		stop := ui.PrimaryButton(c.Key("bar-stop"), "").Size(76, 36).Padding(0).Gap(5).Radius(8).Shrink(0).
			Label("结束").Disabled(!v.running || v.ending).
			Children(func() {
				ui.Icon(c, recordStopIcon).Size(16, 16).AccessHidden(true)
				ui.Text(c, "结束").FontSize(13).SingleLine()
			})
		if stop.Clicked() {
			v.command("finish")
		}
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
		v.invalidate()
	default:
	}
}

// Preserve screen coordinates, including negative origins, so the portal can
// restore the grant for the display under the mouse instead of a global grant.
func recordingStreamOptions(display mygo.Display, recording capture.RecordingOptions) capture.StreamOptions {
	b := display.Bounds
	return capture.StreamOptions{
		DisplayBounds: image.Rect(b.X, b.Y, b.X+b.Width, b.Y+b.Height),
		Recording:     &recording,
	}
}

func (v *videoWindow) start() { v.startRecording(true, "", nil) }

func (v *videoWindow) startRecording(reuse bool, token string, selected *editor.ScreenSelection) {
	if v.closed || v.busy || v.running || v.exporting || v.app.captureBusy {
		return
	}
	if _, err := components.Find("framely-record"); err != nil {
		v.notice = err.Error()
		return
	}
	if v.ctx.Err() != nil {
		v.ctx, v.cancel = context.WithCancel(v.app.ctx)
	}
	v.commands = make(chan string, 4)
	setup := v.app.recordingSetup
	if selected != nil {
		setup.Selection = selected
	}
	var selection *editor.ScreenSelection
	if reuse && setup.Selection != nil {
		copy := *setup.Selection
		selection = &copy
	}
	setup.FPS, setup.Quality = v.fps, v.quality
	fps, _ := strconv.Atoi(v.fps)
	request := recordingRequest{
		setup: setup, selection: selection, fps: fps,
		maxEdge: map[string]int{"720p": 1280, "1080p": 1920, "1440p": 2560, "4K": 3840}[v.quality],
		dir:     v.app.recordDir(), ask: v.app.cfg.AskRecordPath, token: token, commands: v.commands,
	}
	v.busy, v.app.captureBusy = true, true
	v.notice = "正在连接共享来源…"
	v.app.refreshTray()
	ctx := v.ctx
	v.app.worker(func() { v.runRecording(ctx, request) })
}

func (v *videoWindow) exportGIF() {
	if v.exporting || v.gifSource == "" {
		return
	}
	start, duration, valid := v.gifRange()
	fps, _ := strconv.Atoi(v.gifFPS)
	width, _ := strconv.Atoi(v.gifWidth)
	if !valid {
		v.notice = "请修正选段的开始时间和时长。"
		return
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
				v.app.refreshTrayClock()
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
	if err == nil {
		return
	}
	message := err.Error()
	log.Print("recording: ", message)
	notice, path := recordingFailure(err)
	v.later(func() {
		v.diagnostic, v.errorDetails = message, false
		v.notice = notice
		if path != "" {
			v.recoveryPath = path
		}
		if v.running {
			v.quitAfter = false
		}
	})
}

func recordingFailure(err error) (notice, path string) {
	var recovery *recording.RecoveryError
	if errors.As(err, &recovery) && recovery.Path != "" {
		return "录制未能完成，已保留录制片段，可尝试播放或从文件夹中找回。", recovery.Path
	}
	return "录制未能完成，请重试。", ""
}

func (v *videoWindow) openMediaPath(path string) {
	open := v.openPath
	if open == nil {
		open = func(path string) error { return mygo.Shell.OpenPath(path) }
	}
	if err := open(path); err != nil {
		v.notice, v.diagnostic, v.errorDetails = "无法打开，请确认文件仍存在，并已安装可用的播放器或文件管理器。", err.Error(), false
	}
}

func videoPreview(ctx context.Context, path string) image.Image {
	img, _ := videoPreviewAt(ctx, path, 0)
	return img
}

func videoPreviewAt(ctx context.Context, path string, seconds float64) (image.Image, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "framely-video-preview-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "preview.bmp")
	if err := recording.ThumbnailAt(ctx, path, file, seconds); err != nil {
		return nil, err
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return bmp.Decode(f)
}
