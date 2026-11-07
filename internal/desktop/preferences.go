package desktop

import (
	"context"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/config"
	"github.com/wildalley/screenpin/internal/editor"
	"github.com/wildalley/screenpin/internal/niribind"
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
	confirming    bool
	recording     bool
	notice        string
	tab           int
	streamIssue   string
}

func (s *application) showSettings(notice string) {
	if s.scroll != nil && s.scroll.running {
		s.scroll.show()
	}
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
		Title: "ScreenPin 设置与快捷键", Width: 660, Height: 640, MinWidth: 420, MinHeight: 380,
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
	v.refreshAccess()
}

func (v *settingsWindow) refreshAccess() {
	if runtime.GOOS != "linux" || v.accessLoading {
		return
	}
	v.accessLoading = true
	v.app.worker(func() {
		access := capture.ScreenshotAccessContext(v.ctx)
		issue := capture.StreamConfigurationIssue()
		v.later(func() { v.access = access; v.streamIssue = issue; v.accessLoading = false })
	})
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
	panelTheme(c)
	if !v.recording && c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
		v.win.Close()
		return
	}
	s := v.app
	ui.Column(c).Fill().Padding(20).Gap(16).Children(func() {
		ui.Text(c, "设置与快捷键").FontSize(20).FontWeight(600)
		previousTab := v.tab
		ui.Tabs(c, &v.tab, "快捷键", "常规", "权限与长截图")
		if previousTab != v.tab {
			v.recording = false
		}
		ui.Scroll(c.Key("settings-content")).Grow(1).MinHeight(0).Gap(16).Children(func() {
			if v.tab == 0 {
				ui.Column(c).Gap(8).Children(func() {
					ui.Text(c, "截图快捷键").FontWeight(600)
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, config.DisplayShortcut(v.draft.CaptureShortcut)).Font("monospace").Grow(1)
						label := "录制快捷键"
						if v.recording {
							label = "按下组合键…"
						}
						record := ui.Button(c.Key("record"), label).Disabled(v.saving || s.shortcutPending)
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
						if runtime.GOOS == "linux" {
							if niribind.Current() {
								ui.Text(c, "可由 niri 直接管理截图按键，配置前会显示确认框。").FontSize(12).TextColor(c.Theme().TextMuted)
								if ui.Button(c.Key("niri-shortcut"), "配置 niri 快捷键").Disabled(v.saving || s.shortcutPending).Clicked() {
									v.setupNiri()
								}
							} else if ui.Button(c.Key("authorize-shortcut"), "授权快捷键").
								Tooltip("请求系统授权并保存当前快捷键").Disabled(v.saving || s.shortcutPending).Clicked() {
								v.save()
							}
						}
					}
					if s.shortcutPending {
						ui.Text(c, "正在请求系统快捷键授权，请在系统弹窗中确认。若没有弹窗，当前桌面可能不支持此功能。").
							FontSize(12).TextColor(c.Theme().TextMuted)
					}
				})
				ui.Text(c, "图片窗口快捷键").FontWeight(600)
				ui.Text(c, "以下按键仅在图片窗口中生效；输入文字时保留文本编辑操作。").FontSize(12).TextColor(c.Theme().TextMuted)
				for _, row := range editor.ShortcutRows() {
					keyRow(c, row[0], row[1])
				}
				ui.Text(c, "选区操作").FontWeight(600)
				keyRow(c, "拖动 / 空格", "框选 / 移动选区")
				keyRow(c, "Enter / Esc", "确认 / 取消")
			} else if v.tab == 1 {
				ui.Text(c, "启动与图片").FontWeight(600)
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
				ui.Text(c, "ScreenPin "+Version+" · MyGo").FontSize(12).TextColor(c.Theme().TextMuted)
			} else {
				ui.Text(c, "长截图").FontWeight(600)
				ui.Text(c, "长截图需要持续共享所选窗口或显示器；截图快捷键授权与屏幕共享是两个独立权限。").TextColor(c.Theme().TextMuted)
				if v.streamIssue != "" {
					ui.Text(c, v.streamIssue).TextColor(c.Theme().Danger).Selectable()
				} else {
					ui.Text(c, "开始长截图时选择共享来源；选好后框选滚动内容。自动滚动不可用时，可缓慢手动滚动。").FontSize(12).TextColor(c.Theme().TextMuted)
				}
				if ui.Button(c, "打开长截图").Disabled(s.captureBusy).Clicked() {
					s.showScroll()
				}

				if runtime.GOOS == "linux" {
					if ui.Button(c, "重新检查权限").Disabled(v.accessLoading || v.asking).Clicked() {
						v.refreshAccess()
					}
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
			}

			if v.confirming {
				ui.Text(c, "请在配置确认框中选择是否设置 niri 快捷键。").FontSize(12).TextColor(c.Theme().TextMuted)
			} else if v.saving {
				ui.Text(c, "正在应用设置，请完成系统显示的快捷键设置。").FontSize(12).TextColor(c.Theme().TextMuted)
			}
		})
		if v.notice != "" {
			ui.Text(c, v.notice).FontSize(12).MaxHeight(64).Clip().Tooltip(v.notice).Selectable()
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				v.win.Close()
			}
			label := "保存设置"
			if v.confirming {
				label = "等待确认…"
			} else if v.saving {
				label = "保存中…"
			}
			if ui.PrimaryButton(c.Key("save"), label).Disabled(v.saving || s.shortcutPending).Clicked() {
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
	if v.saving || v.app.shortcutPending {
		return
	}
	if err := config.ValidateShortcut(v.draft.CaptureShortcut); err != nil {
		v.notice = err.Error()
		return
	}
	if niribind.Current() && niribind.Installed() && v.draft.CaptureShortcut != v.app.cfg.CaptureShortcut {
		v.setupNiri()
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

// setupNiri is an application configuration confirmation, not a simulated
// system permission dialog. The user sees the key, command and files first.
func (v *settingsWindow) setupNiri() {
	if v.saving || v.app.shortcutPending {
		return
	}
	next := v.draft
	if err := config.ValidateShortcut(next.CaptureShortcut); err != nil {
		v.notice = err.Error()
		return
	}
	v.recording, v.saving, v.confirming, v.notice = false, true, true, ""
	v.app.worker(func() {
		plan, err := niribind.Prepare(next.CaptureShortcut)
		if err != nil {
			v.later(func() { v.saving, v.confirming = false, false; v.notice = err.Error() })
			return
		}
		if v.ctx.Err() != nil {
			return
		}
		result, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: v.win, Type: mygo.MessageQuestion, Title: "配置 niri 截图快捷键",
			Message: "将 " + config.DisplayShortcut(plan.Key) + " 设置为 ScreenPin 截图快捷键？",
			Detail:  "这会在 niri 配置中添加一条 include，并创建 ScreenPin 管理的按键文件。若该组合已有操作，将由 ScreenPin 截图替换；更换按键会恢复原操作。原配置会先备份，取消不做修改。\n\n配置：" + plan.ConfigPath + "\n按键文件：" + plan.ShortcutPath + "\n执行：" + plan.Command + " --capture",
			Buttons: []string{"取消", "配置快捷键"}, DefaultButton: 0, CancelButton: 0,
		})
		v.later(func() {
			v.confirming = false
			if err != nil || result.Button != 1 {
				v.saving = false
				if err != nil {
					v.notice = err.Error()
				}
				return
			}
			v.app.applySettings(v.ctx, next, func(ctx context.Context, cfg config.Config) error {
				return plan.Apply(ctx, func() error { return v.app.shortcuts.Apply(ctx, cfg) })
			}, func(err error) {
				if v.ctx.Err() != nil {
					return
				}
				v.saving = false
				if err != nil {
					v.notice = "配置失败：" + err.Error()
				} else {
					v.notice = "niri 快捷键已配置，请按下组合键验证。"
				}
				v.win.Invalidate()
			})
		})
	})
}

// All shortcut configuration and reference now share the settings window.
func (s *application) showKeys() {
	s.showSettings("")
	s.settings.tab = 0
	s.settings.win.Invalidate()
}

func keyRow(c *ui.Context, key, action string) {
	ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
		ui.Text(c, key).Width(190).Shrink(0).Font("monospace").FontSize(12).Selectable()
		ui.Text(c, action).Grow(1)
	})
}
