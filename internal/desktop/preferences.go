package desktop

import (
	"context"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/config"
	"github.com/wildalley/screenpin/internal/editor"
)

type settingsWindow struct {
	app           *application
	win           *mygo.Window
	ctx           context.Context
	cancel        context.CancelFunc
	draft         config.Config
	autostart     bool
	access        capture.Access
	accessLoading bool
	asking        bool
	saving        bool
	recording     bool
	notice        string
}

func (s *application) showSettings(notice string) {
	if s.settings != nil {
		if notice != "" {
			s.settings.notice = notice
		}
		s.settings.win.Show()
		s.settings.win.Focus()
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	v := &settingsWindow{app: s, ctx: ctx, cancel: cancel, draft: s.cfg, autostart: mygo.App.OpenAtLogin(),
		access: capture.AccessUnsupported, notice: notice}
	v.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "ScreenPin 设置", Width: 520, Height: 460, MinWidth: 420, MinHeight: 350,
		UseContentSize: true, DisableMaximize: true, DisableFullScreen: true, Content: ui.View(v.view),
	})
	s.settings = v
	v.win.OnClosed(func() {
		v.cancel()
		v.recording = false
		if s.settings == v {
			s.settings = nil
		}
		s.maybeQuit()
	})
	if runtime.GOOS == "linux" {
		v.accessLoading = true
		s.worker(func() {
			access := capture.ScreenshotAccessContext(v.ctx)
			v.later(func() { v.access = access; v.accessLoading = false })
		})
	}
}

func (v *settingsWindow) later(fn func()) {
	v.app.dispatch(func() {
		if v.ctx.Err() == nil {
			fn()
			v.win.Invalidate()
		}
	})
}

func (v *settingsWindow) view(c *ui.Context) {
	if !v.recording && c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
		v.win.Close()
		return
	}
	s := v.app
	ui.Column(c).Fill().Padding(20).Gap(16).Children(func() {
		ui.Scroll(c).Grow(1).Gap(16).Children(func() {
			ui.Column(c).Gap(8).Children(func() {
				ui.Text(c, "截图快捷键").FontWeight(600)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, config.DisplayShortcut(v.draft.CaptureShortcut)).Font("monospace").Grow(1)
					label := "录制快捷键"
					if v.recording {
						label = "按下组合键…"
					}
					record := ui.Button(c.Key("record"), label).Disabled(v.saving)
					if v.recording {
						record.Focus().HandleInput(func(ev ui.InputEvent) bool {
							if ev.Kind != ui.InputKeyDown {
								return false
							}
							if ev.Key == ui.KeyEscape {
								v.recording = false
								return true
							}
							if key, ok := accelerator(ev.Mods, ev.Key); ok {
								v.draft.CaptureShortcut = key
								v.recording = false
							}
							return true
						})
					}
					if record.Clicked() {
						v.recording = !v.recording
					}
				})
				ui.Text(c, "当前按键："+s.shortcutDescription).FontSize(12).TextColor(c.Theme().TextMuted)
				if s.shortcutError != "" {
					ui.Text(c, "快捷键不可用："+s.shortcutError+"。可从托盘截图，或绑定 screenpin --capture。").
						FontSize(12).TextColor(c.Theme().Danger)
				}
			})
			ui.Column(c).Gap(8).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					if ui.Switch(c.Key("autostart"), &v.autostart).Label("开机自启").Changed() {
						s.setAutostart(v.autostart)
					}
					ui.Text(c, "开机自启")
				})
				ui.Text(c, "登录后在托盘待命，改动立即生效。").FontSize(12).TextColor(c.Theme().TextMuted)
			})
			ui.Column(c).Gap(8).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Switch(c.Key("close-after-copy"), &v.draft.HideAfterCopy).Label("复制后关闭图片").Disabled(v.saving)
					ui.Text(c, "复制后关闭图片")
				})
				ui.Text(c, "置顶图片继续保留。关闭会清空该图片及其标注。").FontSize(12).TextColor(c.Theme().TextMuted)
			})
			if runtime.GOOS == "linux" {
				ui.Column(c).Gap(8).Children(func() {
					ui.Text(c, "截图权限").FontWeight(600)
					if v.accessLoading {
						ui.Text(c, "正在读取权限…").TextColor(c.Theme().TextMuted)
					} else {
						hint := "使用系统选区界面。"
						switch v.access {
						case capture.AccessGranted:
							hint = "已授权，截图直接进入选区。"
						case capture.AccessUnknown:
							hint = "尚未授权，截图使用系统选区。"
						case capture.AccessDenied:
							hint = "已拒绝，可在系统的应用设置中允许 ScreenPin 截图。"
						}
						ui.Text(c, hint).FontSize(12).TextColor(c.Theme().TextMuted)
						if v.access == capture.AccessUnknown {
							label := "允许直接选区"
							if v.asking {
								label = "等待授权…"
							}
							if ui.Button(c.Key("grant"), label).Disabled(v.asking || s.captureBusy).Clicked() {
								v.askAccess()
							}
						}
					}
				})
			}
			if v.saving {
				ui.Text(c, "正在应用设置，请完成系统显示的快捷键设置。").FontSize(12).TextColor(c.Theme().TextMuted)
			}
			if v.notice != "" {
				ui.Text(c, v.notice).FontSize(12).Selectable()
			}
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				v.win.Close()
			}
			label := "保存设置"
			if v.saving {
				label = "保存中…"
			}
			if ui.PrimaryButton(c.Key("save"), label).Disabled(v.saving).Clicked() {
				v.save()
			}
		})
	})
}

func (v *settingsWindow) askAccess() {
	v.asking = true
	v.win.Focus()
	v.app.worker(func() {
		_, err := capture.ScreenshotContext(v.ctx, false)
		access := capture.ScreenshotAccessContext(v.ctx)
		v.later(func() {
			v.asking = false
			v.access = access
			if err != nil && !cancelled(err) {
				v.notice = err.Error()
			}
		})
	})
}

func (v *settingsWindow) save() {
	if v.saving {
		return
	}
	if err := config.ValidateShortcut(v.draft.CaptureShortcut); err != nil {
		v.notice = err.Error()
		return
	}
	v.recording = false
	v.saving = true
	v.notice = ""
	v.app.saveSettings(v.ctx, v.draft, func(err error) {
		if v.ctx.Err() != nil {
			return
		}
		v.saving = false
		if err != nil {
			v.notice = "保存失败：" + err.Error()
		} else {
			v.notice = "设置已保存"
		}
		v.win.Invalidate()
	})
}

func (s *application) showKeys() {
	if s.keys != nil {
		s.keys.Show()
		s.keys.Focus()
		return
	}
	win := mygo.NewWindow(mygo.WindowOptions{
		Title: "ScreenPin 按键", Width: 600, Height: 680, MinWidth: 480, MinHeight: 360,
		UseContentSize: true, DisableMaximize: true, DisableFullScreen: true,
		Content: ui.View(func(c *ui.Context) {
			if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
				s.keys.Close()
				return
			}
			ui.Column(c).Fill().Padding(20).Gap(16).Children(func() {
				ui.Scroll(c).Grow(1).Gap(16).Children(func() {
					ui.Text(c, "全局截图").FontWeight(600)
					keyRow(c, s.shortcutDescription, "截取屏幕")
					if s.shortcutError != "" {
						ui.Text(c, "可从托盘截图，或在系统中绑定 screenpin --capture。").FontSize(12).TextColor(c.Theme().TextMuted)
					}
					ui.Text(c, "图片窗口").FontWeight(600)
					ui.Column(c).Gap(8).Children(func() {
						for _, row := range editor.ShortcutRows() {
							keyRow(c, row[0], row[1])
						}
					})
					ui.Text(c, "选区").FontWeight(600)
					ui.Column(c).Gap(8).Children(func() {
						keyRow(c, "拖动后松开", "截取区域")
						keyRow(c, "拖动时按住空格", "移动选区")
						keyRow(c, "Enter", "截取整块显示器")
						keyRow(c, "Esc / 右键", "取消选区")
					})
				})
				ui.Row(c).Justify(ui.End).Children(func() {
					if ui.Button(c, "关闭").Clicked() {
						s.keys.Close()
					}
				})
			})
		}),
	})
	s.keys = win
	win.OnClosed(func() {
		if s.keys == win {
			s.keys = nil
		}
		s.maybeQuit()
	})
}

func keyRow(c *ui.Context, key, action string) {
	ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
		ui.Text(c, key).Width(230).Font("monospace").FontSize(12).Selectable()
		ui.Text(c, action).Grow(1)
	})
}
