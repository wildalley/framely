package desktop

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"log"
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/components"
	"github.com/wildalley/screenpin/internal/editor"
	"github.com/wildalley/screenpin/internal/scrollshot"
)

type scrollCommand struct {
	kind      string
	automatic bool
}

func (s *application) blockPictureDuringScroll() bool {
	if s.scroll == nil || !s.scroll.running {
		return false
	}
	s.scroll.show()
	s.scroll.notice = "请先完成或取消长截图，再打开或粘贴其他图片。"
	s.scroll.win.Invalidate()
	return true
}

type scrollWindow struct {
	app                                      *application
	win                                      *mygo.Window
	ctx                                      context.Context
	cancel                                   context.CancelFunc
	commands                                 chan scrollCommand
	closed, busy, running, paused, ending    bool
	loading, canAuto, automatic, autoGranted bool
	sourceEnded                              bool
	monitors                                 []capture.Monitor
	monitorName                              string
	notice, setupError                       string
	preview                                  *ui.Bitmap
	previewSize                              image.Point
	height                                   int
	canUndo                                  bool
	hidden                                   []*mygo.Window
}

func (s *application) showScroll() {
	if !capture.ScrollingSupported() {
		s.notify("当前系统暂不支持长截图，截图、标注和离线 OCR 可正常使用。")
		return
	}
	if s.scroll != nil {
		s.scroll.show()
		return
	}
	if s.captureBusy {
		s.notify("请先完成当前截图。")
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	v := &scrollWindow{app: s, ctx: ctx, cancel: cancel, commands: make(chan scrollCommand, 8), loading: true}
	s.scroll = v
	s.operations++
	v.win = mygo.NewWindow(mygo.WindowOptions{Title: "ScreenPin 长截图", Width: 620, Height: 620, MinWidth: 380, MinHeight: 380, UseContentSize: true, Content: ui.View(v.view)})
	v.win.OnClosed(v.release)
	v.checkSetup()
}
func (v *scrollWindow) checkSetup() {
	if v.busy || v.running {
		return
	}
	v.loading = true
	v.app.worker(func() {
		_, err := components.Find("screenpin-capture")
		if issue := capture.StreamConfigurationIssue(); err == nil && issue != "" {
			err = errors.New(issue)
		}
		monitors, listErr := capture.StreamMonitors(v.ctx)
		if err == nil {
			err = listErr
		}
		canAuto := err == nil && capture.AutomaticScrollAvailable(v.ctx)
		v.later(func() {
			v.loading = false
			v.canAuto, v.automatic = canAuto, canAuto
			v.monitors = monitors
			if len(monitors) > 0 {
				v.monitorName = monitors[0].Name
				for _, m := range monitors {
					if m.UnderMouse {
						v.monitorName = m.Name
					}
				}
			}
			v.setupError = ""
			if err != nil {
				v.setupError = err.Error()
			}
		})
	})
}
func (v *scrollWindow) later(fn func()) {
	v.app.dispatch(func() {
		if !v.closed {
			fn()
			v.win.Invalidate()
		}
	})
}
func (v *scrollWindow) release() {
	if v.closed {
		return
	}
	v.closed = true
	v.cancel()
	v.restore()
	v.preview = nil
	v.previewSize = image.Point{}
	v.monitors = nil
	if v.app.scroll == v {
		v.app.scroll = nil
		v.app.captureBusy = false
		v.app.operations--
		v.app.refreshTray()
		v.app.maybeQuit()
	}
}
func (v *scrollWindow) restore() {
	for _, w := range v.hidden {
		if !w.IsDestroyed() && w != v.win {
			w.ShowInactive()
		}
	}
	v.hidden = nil
}

func (v *scrollWindow) hideAll() {
	for _, w := range mygo.Windows() {
		if !w.IsVisible() {
			continue
		}
		found := false
		for _, old := range v.hidden {
			if old == w {
				found = true
				break
			}
		}
		if !found {
			v.hidden = append(v.hidden, w)
		}
		w.Hide()
	}
}
func (v *scrollWindow) send(kind string) {
	if v.closed {
		return
	}
	select {
	case v.commands <- scrollCommand{kind: kind, automatic: v.automatic}:
	default:
	}
}
func (v *scrollWindow) show() {
	if v.running && !v.paused {
		v.paused = true
		v.send("pause")
	}
	v.win.Show()
	v.win.Focus()
}
func (v *scrollWindow) finish() {
	if !v.running || v.ending {
		return
	}
	v.ending = true
	v.send("finish")
	v.win.Show()
	v.win.Invalidate()
}

func (v *scrollWindow) view(c *ui.Context) {
	panelTheme(c)
	if v.closed {
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyW) || c.Shortcut(0, ui.KeyEscape) {
		v.win.Close()
		return
	}
	ui.Column(c).Fill().Padding(18).Gap(12).Children(func() {
		ui.Scroll(c).Grow(1).MinHeight(0).Gap(12).Children(func() {
			ui.Text(c, "长截图").FontSize(20).FontWeight(600)
			if !v.running {
				ui.Text(c, "选择窗口或显示器，再框选滚动内容。截图过程中可从托盘查看、暂停或完成。").TextColor(c.Theme().TextMuted)
				if v.loading {
					ui.Text(c, "正在检查采集组件…")
				}
				if len(v.monitors) > 0 {
					names := make([]string, len(v.monitors))
					for i, m := range v.monitors {
						names[i] = m.Name
					}
					ui.Select(c, &v.monitorName, names).Label("采集显示器").FillWidth().Disabled(v.busy)
				}
			} else {
				ui.Text(c, fmt.Sprintf("已拼接高度：%d px", v.height)).Font("monospace")
			}
			auto := ui.Checkbox(c.Key("automatic"), &v.automatic, "自动向下滚动").Disabled(v.loading || v.busy || !v.canAuto || v.sourceEnded || v.running && !v.autoGranted)
			if auto.Changed() && v.running {
				v.paused = true
				v.send("pause")
			}
			if !v.loading && !v.canAuto {
				ui.Text(c, "当前桌面未提供自动滚动所需的指针控制接口，可使用手动模式。").FontSize(12).TextColor(c.Theme().TextMuted)
			}
			if v.running {
				ui.Text(c, "再次按全局截图快捷键可完成；从托盘打开此窗口会暂停。手动模式请缓慢向下滚动。").FontSize(12).TextColor(c.Theme().TextMuted)
			}
			if v.setupError != "" {
				ui.Text(c, v.setupError).TextColor(c.Theme().Danger).Selectable()
			}
			if v.notice != "" {
				ui.Text(c, v.notice).TextColor(c.Theme().TextMuted).Selectable()
			}
			if !v.running {
				if ui.Button(c, "权限与长截图设置").Clicked() {
					v.app.showSettings("")
					v.app.settings.tab = 2
					v.app.settings.win.Invalidate()
				}
			}
			if v.preview != nil {
				ui.Box(c).Height(300).FillWidth().Clip().Background(c.Theme().Surface).Draw(func(p *ui.Painter, r ui.Rect) {
					if v.preview != nil {
						v.drawPreview(p, r)
					}
				})
			} else {
				ui.Column(c).Padding(16).Gap(10).FillWidth().Background(c.Theme().Surface).Children(func() {
					ui.Text(c, "1. 选择共享的窗口或显示器")
					ui.Text(c, "2. 框选需要滚动的内容")
					ui.Text(c, "3. 缓慢滚动，再按截图快捷键完成")
				})
			}
		})
		ui.Row(c).Gap(8).Wrap().Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Disabled(v.ending).Clicked() {
				v.win.Close()
			}
			if v.running {
				if ui.Button(c, "撤回最后一段").Disabled(!v.canUndo || v.ending).Clicked() {
					v.paused = true
					v.send("undo")
				}
				if ui.Button(c, "继续截图").Disabled(!v.paused || v.ending || v.sourceEnded).Clicked() {
					v.paused = false
					v.hideAll()
					v.send("resume")
				}
				if ui.PrimaryButton(c, "完成截图").Disabled(v.ending).Clicked() {
					v.finish()
				}
			} else {
				if v.setupError != "" && ui.Button(c, "重新检查").Disabled(v.loading).Clicked() {
					v.checkSetup()
				}
				label := "选择截图区域"
				if v.busy {
					label = "正在选择…"
				}
				if ui.PrimaryButton(c, label).Disabled(v.loading || v.busy || v.setupError != "").Clicked() {
					v.start()
				}
			}
		})
	})
}

func (v *scrollWindow) start() {
	if v.closed || v.busy || v.running {
		return
	}
	v.busy = true
	v.notice = "正在打开系统共享选择器，请选择窗口或显示器。若未出现选择器，可取消后检查权限与长截图设置。"
	v.app.captureBusy = true
	v.app.refreshTray()
	options := capture.StreamOptions{Automatic: v.automatic}
	for _, m := range v.monitors {
		if m.Name == v.monitorName {
			options.Monitor = m.ID
		}
	}
	// Keep the controller and Cancel available while the system chooser opens.
	for _, w := range mygo.Windows() {
		if w != v.win && w.IsVisible() {
			v.hidden = append(v.hidden, w)
			w.Hide()
		}
	}
	v.win.Invalidate()
	v.app.worker(func() {
		timer := time.NewTimer(180 * time.Millisecond)
		select {
		case <-v.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		source, err := capture.OpenStream(v.ctx, options)
		if err != nil {
			v.startFailed(err)
			return
		}
		defer source.Close()
		v.later(func() { v.notice = "正在连接共享画面…"; v.hideAll() })
		firstCtx, done := context.WithTimeout(v.ctx, 15*time.Second)
		first, err := source.Next(firstCtx)
		done()
		if err != nil {
			v.startFailed(err)
			return
		}
		// Let the portal's source chooser disappear before taking the frame
		// used for region selection; this is still the same capture session.
		timer = time.NewTimer(350 * time.Millisecond)
		select {
		case <-v.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		freshCtx, stop := context.WithTimeout(v.ctx, 500*time.Millisecond)
		if fresh, e := source.Next(freshCtx); e == nil {
			first = fresh
		}
		stop()
		region, err := editor.SelectImageRegion(v.ctx, first, "选择长截图的滚动区域")
		if err != nil {
			v.startFailed(err)
			return
		}
		if region.Dx() < 80 || region.Dy() < 160 {
			v.startFailed(errors.New("长截图选区至少需要 80 × 160 像素，请扩大滚动区域"))
			return
		}
		controller, err := scrollshot.NewController(first.SubImage(region).(*image.RGBA), options.Automatic, time.Now())
		if err != nil {
			v.startFailed(err)
			return
		}
		defer controller.Stitch.Release()
		sourceSize := first.Bounds().Size()
		first = nil
		v.later(func() { v.busy = false; v.running = true; v.autoGranted = options.Automatic; v.app.refreshTray() })
		v.publish(controller, false)
		finished := v.run(source, region, sourceSize, controller)
		if !finished || v.ctx.Err() != nil {
			return
		}
		pixels := controller.Stitch.Flatten()
		source.Close()
		v.later(func() {
			picture, err := editor.PictureFromPixels(pixels, "长截图 "+time.Now().Format("15-04-05"))
			if err != nil {
				v.notice = err.Error()
				v.ending = false
				return
			}
			v.app.operations++ // keep no-tray mode alive during window handoff
			v.win.Destroy()
			v.app.showPicture(picture, nil, "")
			v.app.operations--
			v.app.maybeQuit()
		})
	})
}

func (v *scrollWindow) startFailed(err error) {
	if !cancelled(err) {
		log.Printf("ScreenPin: long screenshot: %v", err)
	}
	v.later(func() {
		v.busy = false
		v.app.captureBusy = false
		v.restore()
		v.win.Show()
		v.app.refreshTray()
		if !cancelled(err) {
			v.notice = err.Error()
		}
	})
}

func (v *scrollWindow) publish(c *scrollshot.Controller, show bool) {
	preview := c.Stitch.Preview(previewWidth, previewHeight)
	height, canUndo, paused, automatic, notice := c.Stitch.Height(), c.Stitch.CanUndo(), c.Paused, c.Automatic, c.Notice
	v.later(func() {
		v.setPreview(preview)
		v.height, v.canUndo, v.paused, v.automatic, v.notice = height, canUndo, paused, automatic, notice
		if show {
			v.win.Show()
			v.win.Focus()
		}
		v.app.refreshTray()
	})
}

const previewWidth, previewHeight = 720, 820

func (v *scrollWindow) setPreview(preview *image.RGBA) {
	// Fixed texture dimensions let Bitmap.Update reuse the GPU allocation
	// even when the stitched image's aspect ratio changes on every segment.
	pixels := image.NewRGBA(image.Rect(0, 0, previewWidth, previewHeight))
	v.previewSize = preview.Bounds().Size()
	draw.Draw(pixels, image.Rectangle{Max: v.previewSize}, preview, preview.Rect.Min, draw.Src)
	if v.preview == nil {
		v.preview = ui.NewBitmap(pixels)
	} else {
		v.preview.Update(pixels)
	}
}

func (v *scrollWindow) drawPreview(p *ui.Painter, r ui.Rect) {
	w, h := float32(v.previewSize.X), float32(v.previewSize.Y)
	if w <= 0 || h <= 0 {
		return
	}
	scale := float32(math.Min(float64(r.W/w), float64(r.H/h)))
	dst := ui.Rect{X: r.X + (r.W-w*scale)/2, Y: r.Y + (r.H-h*scale)/2, W: w * scale, H: h * scale}
	p.ImageRegion(v.preview, ui.Rect{W: w, H: h}, dst, ui.ImageOptions{})
}

func (v *scrollWindow) run(source capture.Stream, region image.Rectangle, sourceSize image.Point, c *scrollshot.Controller) bool {
	processCommands := func() (finish bool) {
		for {
			select {
			case command := <-v.commands:
				switch command.kind {
				case "finish":
					return true
				case "pause":
					c.Pause("已暂停，可查看结果或继续截图。")
					v.publish(c, false)
				case "resume":
					c.Resume(command.automatic, time.Now())
				case "undo":
					c.Undo()
					v.publish(c, true)
				}
			default:
				return false
			}
		}
	}
	for v.ctx.Err() == nil {
		if processCommands() {
			return true
		}
		frameCtx, done := context.WithTimeout(v.ctx, 100*time.Millisecond)
		frame, err := source.Next(frameCtx)
		done()
		if processCommands() {
			return true
		}
		if frame != nil && frame.Bounds().Size() != sourceSize {
			err = scrollshot.ErrSize
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			if v.ctx.Err() != nil {
				return false
			}
			c.Pause("画面采集已结束，可完成并保存此前结果：" + err.Error())
			source.Close()
			v.later(func() { v.sourceEnded = true })
			v.publish(c, true)
			// Keep the finished preview reviewable without reading a dead stream.
			for {
				select {
				case <-v.ctx.Done():
					return false
				case command := <-v.commands:
					if command.kind == "finish" {
						return true
					}
					if command.kind == "undo" {
						c.Undo()
						v.publish(c, false)
					}
				}
			}
		}
		if frame != nil {
			if !region.In(frame.Bounds()) {
				c.Pause(scrollshot.ErrSize.Error())
				v.publish(c, true)
				continue
			}
			c.Feed(frame.SubImage(region).(*image.RGBA), time.Now())
		}
		changed, scroll := c.Tick(time.Now())
		if scroll {
			err := source.Scroll(v.ctx, image.Pt(region.Min.X+region.Dx()/2, region.Min.Y+region.Dy()/2))
			c.Scrolled(time.Now(), err)
			if err != nil {
				changed = true
			}
		}
		if changed {
			v.publish(c, c.Paused)
		}
	}
	return false
}
