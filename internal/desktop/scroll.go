package desktop

import (
	"context"
	"errors"
	"fmt"
	"github.com/wildalley/framely/internal/brand"
	"image"
	"image/draw"
	"log"
	"math"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/capture"
	"github.com/wildalley/framely/internal/components"
	"github.com/wildalley/framely/internal/editor"
	"github.com/wildalley/framely/internal/scrollshot"
)

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
	commands                                 chan scrollshot.Command
	closed, busy, running, paused, ending    bool
	loading, canAuto, automatic, autoGranted bool
	sourceEnded                              bool
	monitors                                 []capture.Monitor
	monitorName                              string
	notice, setupError                       string
	preview                                  *ui.Bitmap
	previewBuffers                           [2]*image.RGBA
	previewNext                              int
	previewSize                              image.Point
	height                                   int
	canUndo                                  bool
	hidden                                   []*mygo.Window
	floating                                 bool
	holdsCapture                             bool
	display                                  mygo.Display
	overlay, regionFrame, previewOverlay     *mygo.Window
	overlayRegion, overlayToolbar            ui.Rect
	overlayPreview, frameHole                ui.Rect
	reuse                                    *editor.ScreenSelection
}

func (s *application) showScroll() {
	s.showScrollForSelection(nil)
}

func (s *application) showScrollForSelection(selection *editor.ScreenSelection) {
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
	v := &scrollWindow{app: s, ctx: ctx, cancel: cancel, commands: make(chan scrollshot.Command, 8), loading: true, holdsCapture: true, display: mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())}
	if selection != nil && selection.Region.Dx() >= 80 && selection.Region.Dy() >= 160 {
		for _, display := range mygo.Screen.Displays() {
			if display.Bounds == selection.Display {
				copy := *selection
				v.reuse, v.display = &copy, display
				break
			}
		}
	}
	s.scroll = v
	s.captureBusy = true
	s.operations++
	v.win = mygo.NewWindow(mygo.WindowOptions{Title: brand.Title("长截图"), Width: 620, Height: 620, MinWidth: 380, MinHeight: 140, UseContentSize: true, AlwaysOnTop: true, Hidden: true, Content: ui.View(v.view)})
	v.floating = capture.ExcludeWindow(v.win.NativeHandle())
	if runtime.GOOS == "windows" {
		v.win.Hide()
	}
	v.win.OnClosed(v.release)
	s.refreshTray()
	v.checkSetup()
}
func (v *scrollWindow) checkSetup() {
	if v.busy || v.running {
		return
	}
	v.loading = true
	displays := mygo.Screen.Displays()
	v.app.worker(func() {
		_, err := components.FindProtocol("framely-capture", components.FrameProtocol)
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
			v.canAuto, v.automatic = canAuto, canAuto && v.app.cfg.AutomaticScroll
			v.monitors = monitors
			if len(monitors) > 0 {
				v.monitorName = monitors[0].Name
				for _, m := range monitors {
					if m.UnderMouse {
						v.monitorName = m.Name
					}
				}
				// Resolve the source using the display under the pointer before
				// opening any ScreenPin window, not the pointer during setup.
				for i, d := range displays {
					if d.ID == v.display.ID && i < len(monitors) {
						v.monitorName = monitors[i].Name
					}
				}
			}
			v.setupError = ""
			if err != nil {
				v.setupError = err.Error()
				v.holdsCapture, v.app.captureBusy = false, false
				v.app.refreshTray()
				v.win.Show()
			} else {
				v.start()
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
func (v *scrollWindow) invalidateOverlays() {
	for _, w := range []*mygo.Window{v.overlay, v.regionFrame, v.previewOverlay} {
		if w != nil && !w.IsDestroyed() {
			w.Invalidate()
		}
	}
}

func (v *scrollWindow) release() {
	if v.closed {
		return
	}
	v.closed = true
	v.cancel()
	for _, w := range []*mygo.Window{v.overlay, v.regionFrame, v.previewOverlay} {
		if w != nil && !w.IsDestroyed() {
			w.Destroy()
		}
	}
	v.overlay, v.regionFrame, v.previewOverlay = nil, nil, nil
	v.restore()
	v.preview = nil
	v.previewBuffers = [2]*image.RGBA{}
	v.previewNext = 0
	v.previewSize = image.Point{}
	v.monitors = nil
	if v.app.scroll == v {
		v.app.scroll = nil
		if v.holdsCapture {
			v.app.captureBusy = false
		}
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
	case v.commands <- scrollshot.Command{Kind: kind, Automatic: v.automatic}:
	default:
	}
}
func (v *scrollWindow) show() {
	if v.overlay != nil {
		v.overlay.ShowInactive()
		return
	}
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
	log.Print(brand.Name + ": long screenshot finish requested")
	v.send("finish")
	if v.overlay != nil {
		v.invalidateOverlays()
	} else {
		v.win.Show()
	}
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
	if v.running && c.Shortcut(0, ui.KeyEnter) {
		v.finish()
		return
	}
	if v.running && v.floating {
		v.floatingView(c)
		return
	}
	t := c.Theme()
	ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
		panelHeader(c, "长截图", "框选滚动区域，自动拼接", func() {
			state := "就绪"
			switch {
			case v.ending:
				state = "正在生成…"
			case v.paused:
				state = "已暂停"
			case v.running:
				state = "采集中"
			case v.busy || v.loading:
				state = "准备中"
			}
			badge := ui.Badge(c, state).Shrink(0)
			if v.running && !v.paused {
				badge.Background(brand.Signal).TextColor(t.AccentText)
			}
		})
		ui.Scroll(c.Key("scroll-content")).Grow(1).MinHeight(0).Gap(12).Children(func() {
			if !v.running {
				ui.Text(c, "框选滚动内容，松开立即开始。截图过程中可从托盘查看或完成。").FontSize(12).TextColor(t.TextMuted)
				if v.loading {
					ui.Text(c, "正在检查采集组件…").FontSize(12).TextColor(t.TextMuted)
				}
				if len(v.monitors) > 0 {
					names := make([]string, len(v.monitors))
					for i, m := range v.monitors {
						names[i] = m.Name
					}
					ui.Column(c).Gap(5).Children(func() {
						ui.Text(c, "采集显示器").FontSize(12)
						ui.Select(c, &v.monitorName, names).Label("采集显示器").FillWidth().Disabled(v.busy)
					})
				}
			} else {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(10, 12).Radius(8).Background(t.Surface).Border(1, t.Border).Children(func() {
					ui.Box(c).Size(8, 8).Radius(4).Background(v.overlayStatusDot(t)).Shrink(0)
					ui.Text(c, fmt.Sprintf("已拼接高度：%d px", v.height)).Font("monospace").FontWeight(600)
				})
			}
			auto := ui.Checkbox(c.Key("automatic"), &v.automatic, "自动向下滚动").Disabled(v.loading || v.busy || !v.canAuto || v.sourceEnded || v.running && !v.autoGranted)
			if auto.Changed() && v.running {
				v.paused = true
				v.send("pause")
			}
			if !v.loading && !v.canAuto {
				ui.Text(c, "当前桌面未提供自动滚动所需的指针控制接口，可使用手动模式。").FontSize(12).TextColor(t.TextMuted)
			}
			if v.running {
				ui.Text(c, "再次按全局截图快捷键可完成；从托盘打开此窗口会暂停。手动模式可直接滚动或拖动页面滚动条。").FontSize(12).TextColor(t.TextMuted)
			}
			if v.setupError != "" {
				ui.Text(c, v.setupError).FontSize(12).TextColor(t.Danger).Selectable()
			}
			if v.notice != "" {
				ui.Text(c, v.notice).FontSize(12).TextColor(t.TextMuted).Selectable()
			}
			if v.running && v.canAuto && !v.autoGranted {
				ui.Text(c, "本次为手动采集。可在自动滚动设置中开启，下次开始时授权鼠标控制。").FontSize(12).TextColor(t.TextMuted).MaxLines(3)
			}
			ui.Row(c).Children(func() {
				if ui.Button(c, "自动滚动与权限设置").Clicked() {
					v.openModeSettings()
				}
			})
			if v.preview != nil {
				ui.Box(c).Height(300).FillWidth().Radius(8).Clip().Background(t.Surface).Border(1, t.Border).Draw(func(p *ui.Painter, r ui.Rect) {
					if v.preview != nil {
						v.drawPreview(p, r)
					}
				})
			} else {
				ui.Column(c).Padding(14, 16).Gap(8).FillWidth().Radius(8).Background(t.Surface).Border(1, t.Border).Children(func() {
					ui.Text(c, "操作步骤").FontSize(12).FontWeight(600).TextColor(t.TextMuted)
					ui.Text(c, "1. 选择共享的窗口或显示器").FontSize(13)
					ui.Text(c, "2. 框选需要滚动的内容").FontSize(13)
					ui.Text(c, "3. 滚动页面，再按截图快捷键完成").FontSize(13)
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
					v.resume()
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

// Changing the pointer grant requires a new capture session. Keep the current
// stitched result intact while the user changes the preference for next time.
func (v *scrollWindow) openModeSettings() {
	if v.running && !v.paused {
		v.paused = true
		v.send("pause")
		v.invalidateOverlays()
	}
	v.app.showSettings("")
	v.app.settings.tab = 3
	v.app.settings.win.Invalidate()
}

func (v *scrollWindow) resume() {
	v.paused = false
	if !v.floating {
		v.hideAll()
	}
	v.invalidateOverlays()
	v.send("resume")
}

func (v *scrollWindow) floatingView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Padding(12).Gap(8).Background(t.Background).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(v.overlayStatusDot(t)).Shrink(0)
			ui.Text(c, fmt.Sprintf("长截图 · %d px", v.height)).FontWeight(600).Grow(1)
			auto := ui.Checkbox(c, &v.automatic, "自动滚动").Disabled(!v.autoGranted || v.sourceEnded || v.ending)
			if auto.Changed() {
				v.paused = true
				v.invalidateOverlays()
				v.send("pause")
			}
		})
		status := "手动滚动页面；按截图快捷键完成"
		if v.automatic {
			status = "正在自动滚动；按截图快捷键完成"
		}
		if v.paused {
			status = "已暂停，可继续或完成截图"
		}
		if v.ending {
			status = "正在生成长截图…"
		}
		if v.notice != "" {
			status = v.notice
		}
		ui.Row(c).Grow(1).MinHeight(0).Gap(8).AlignItems(ui.Center).Children(func() {
			if v.preview != nil {
				ui.Box(c.Key("floating-preview")).Size(48, 52).Radius(6).Clip().
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
		ui.Row(c).Gap(6).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Radius(8).Disabled(v.ending).Clicked() {
				v.win.Close()
			}
			if ui.Button(c, "撤回").Radius(8).Disabled(!v.canUndo || v.ending).Clicked() {
				v.paused = true
				v.send("undo")
			}
			if v.paused {
				if ui.Button(c, "继续").Radius(8).Disabled(v.sourceEnded || v.ending).Clicked() {
					v.resume()
				}
			} else if ui.Button(c, "暂停").Radius(8).Disabled(v.ending).Clicked() {
				v.paused = true
				v.send("pause")
			}
			if ui.PrimaryButton(c, "完成").Radius(8).Disabled(v.ending).Clicked() {
				v.finish()
			}
		})
	})
}

func (v *scrollWindow) start() {
	if v.closed || v.busy || v.running {
		return
	}
	if v.app.settings != nil && v.app.settings.asking {
		v.notice = "正在等待截图授权，请先完成系统提示。"
		v.win.Show()
		v.win.Invalidate()
		return
	}
	if v.app.captureBusy && !v.holdsCapture {
		v.notice = "请先完成当前截图，再开始长截图。"
		v.win.Invalidate()
		return
	}
	v.busy = true
	v.holdsCapture = true
	v.notice = "正在打开系统共享选择器，请选择窗口或显示器。"
	v.app.captureBusy = true
	v.app.refreshTray()
	options := capture.StreamOptions{Automatic: v.automatic}
	displays := mygo.Screen.Displays()
	for i, m := range v.monitors {
		if m.Name == v.monitorName {
			options.Monitor = m.ID
			if i < len(displays) {
				v.display = displays[i]
			}
		}
	}
	b := v.display.Bounds
	options.DisplayBounds = image.Rect(b.X, b.Y, b.X+b.Width, b.Y+b.Height)
	if runtime.GOOS == "linux" {
		// Wayland may return stale global pointer coordinates. The actual
		// region selection identifies the display before source restoration.
		if v.reuse == nil {
			options.DisplayBounds = image.Rectangle{}
		}
		v.notice = "正在准备框选；首次共享时请选择框选所在的显示器。"
	}
	// Keep Cancel available during portal authorization; Windows has no chooser.
	for _, w := range mygo.Windows() {
		if w != v.win && w.IsVisible() {
			v.hidden = append(v.hidden, w)
			w.Hide()
		}
	}
	v.win.Hide()
	v.win.Invalidate()
	request := scrollRequest{options: options, display: v.display, displays: displays}
	if v.reuse != nil {
		copy := *v.reuse
		request.reuse = &copy
	}
	ctx := v.ctx
	v.app.worker(func() { v.runScrollCapture(ctx, request) })
}

func (v *scrollWindow) startFailed(err error) {
	if !cancelled(err) {
		log.Printf(brand.Name+": long screenshot: %v", err)
	}
	v.later(func() { v.applyStartFailure(err) })
}

func (v *scrollWindow) applyStartFailure(err error) {
	if cancelled(err) {
		log.Print(brand.Name + ": long screenshot cancelled; discarding controller")
		if v.win != nil {
			v.win.Destroy()
		} else {
			v.release()
		}
		return
	}
	v.busy = false
	v.holdsCapture = false
	v.app.captureBusy = false
	v.restore()
	v.win.Show()
	v.app.refreshTray()
	v.notice = err.Error()
}

const previewWidth, previewHeight = scrollshot.PreviewWidth, scrollshot.PreviewHeight

func (v *scrollWindow) setPreview(preview *image.RGBA) {
	// Fixed texture dimensions let Bitmap.Update reuse the GPU allocation
	// even when the stitched image's aspect ratio changes on every segment.
	// Bitmap.Update retains the supplied pixels. Alternate two fixed buffers
	// so preparing the next preview never edits the bitmap currently displayed.
	index := v.previewNext
	pixels := v.previewBuffers[index]
	if pixels == nil {
		pixels = image.NewRGBA(image.Rect(0, 0, previewWidth, previewHeight))
		v.previewBuffers[index] = pixels
	} else {
		clear(pixels.Pix)
	}
	v.previewNext = 1 - index
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
