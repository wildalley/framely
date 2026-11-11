package desktop

import (
	"context"
	"github.com/wildalley/framely/internal/brand"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/capture"
	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/editor"
	"github.com/wildalley/framely/internal/niribind"
	"github.com/wildalley/framely/internal/translate"
)

type settingsWindow struct {
	app             *application
	win             *mygo.Window
	ctx             context.Context
	cancel          context.CancelFunc
	draft           config.Config
	autostart       bool
	access          capture.Access
	accessLoading   bool
	asking          bool
	saving          bool
	confirming      bool
	recording       bool
	recordingScroll bool
	recordingVideo  bool
	recordingMods   ui.Modifiers
	shortcutHint    string
	shortcutInvalid bool
	notice          string
	tab             int
	streamIssue     string

	// Translation keys being entered; empty keeps what is stored.
	trID, trKey string
	trStored    map[string]bool // provider → a key is stored
	trTesting   bool
	trResult    string
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
		Title: brand.Title("设置"), Width: 660, Height: 640, MinWidth: 420, MinHeight: 380,
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
	if !v.recording && (c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW)) {
		v.win.Close()
		return
	}
	s := v.app
	ui.Column(c).Fill().Padding(20).Gap(16).Children(func() {
		panelHeader(c, "设置", brand.Name+" "+brand.ChineseName+" "+Version, nil)
		previousTab := v.tab
		ui.Tabs(c, &v.tab, "快捷键", "常规", "录屏", "权限与长截图", "翻译")
		if previousTab != v.tab {
			v.stopRecording()
		}
		ui.Scroll(c.Key("settings-content")).Grow(1).MinHeight(0).Gap(16).Children(func() {
			if v.tab == 0 {
				ui.Column(c).Gap(8).Children(func() {
					if s.shortcutError != "" {
						ui.Text(c, "快捷键不可用："+s.shortcutError).FontSize(12).TextColor(c.Theme().Danger)
						if runtime.GOOS == "linux" {
							if niribind.Current() {
								if ui.Button(c.Key("niri-shortcut"), "配置 niri 快捷键").Disabled(v.saving || s.shortcutPending || v.recording).Clicked() {
									v.setupNiri()
								}
							} else if ui.Button(c.Key("authorize-shortcut"), "授权快捷键").Disabled(v.saving || s.shortcutPending || v.recording).Clicked() {
								v.save()
							}
						}
					}
					if s.shortcutPending {
						ui.Text(c, "正在请求系统快捷键授权，请在系统弹窗中确认。").FontSize(12).TextColor(c.Theme().TextMuted)
					}
					if s.shortcutSystemManaged {
						ui.Text(c, "全局快捷键").FontWeight(600)
						globalKeyRow(c, s.shortcutDescription, "截图")
						if capture.ScrollingSupported() {
							key := s.scrollShortcutDescription
							if key == "" {
								key = "未绑定"
							}
							globalKeyRow(c, key, "长截图")
						}
						globalKeyRow(c, s.recordShortcutDescription, "录屏")
						ui.Text(c, "再次按下可结束长截图或录屏；系统改键后自动更新。").FontSize(12).TextColor(c.Theme().TextMuted)
						if ui.Button(c, "配置系统快捷键").Disabled(v.saving || s.shortcutPending).Clicked() {
							v.configureShortcuts()
						}
					} else {
						ui.Text(c, "截图快捷键").FontWeight(600)
						ui.Column(c.Key("capture-shortcut")).Gap(8).Children(func() { v.shortcutEntry(c, false, false) })
						ui.Text(c, "当前按键："+s.shortcutDescription).FontSize(12).TextColor(c.Theme().TextMuted)
						if capture.ScrollingSupported() {
							ui.Text(c, "长截图快捷键").FontWeight(600)
							ui.Column(c.Key("scroll-shortcut")).Gap(8).Children(func() { v.shortcutEntry(c, true, false) })
							ui.Text(c, "留空不绑定。按下开始长截图，再次按下完成。").FontSize(12).TextColor(c.Theme().TextMuted)
						}
						ui.Text(c, "录屏快捷键").FontWeight(600)
						ui.Column(c.Key("record-shortcut")).Gap(8).Children(func() { v.shortcutEntry(c, false, true) })
						ui.Text(c, "留空不绑定。开始录屏并沿用上次区域，再次按下结束。").FontSize(12).TextColor(c.Theme().TextMuted)
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
				ui.Text(c, brand.Name+" "+brand.ChineseName+" "+Version+" · MyGo").FontSize(12).TextColor(c.Theme().TextMuted)
			} else if v.tab == 2 {
				ui.Text(c, "录屏").FontWeight(600)
				ui.Text(c, "区域录制为无声 MP4，支持 30/60 fps；录完可在录屏窗口或托盘「视频转 GIF…」导出。").FontSize(12).TextColor(c.Theme().TextMuted)
				ui.Column(c).Gap(6).Children(func() {
					ui.Text(c, "保存位置").FontSize(12)
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						dir := v.draft.RecordDir
						if dir == "" {
							dir = s.recordDirFor("") + "（默认）"
						}
						ui.Text(c, dir).FontSize(12).MaxLines(2).Grow(1).MinWidth(0).Tooltip(dir).Selectable()
						if ui.Button(c.Key("record-dir"), "更改…").Disabled(v.saving).Clicked() {
							v.chooseRecordDir()
						}
						if v.draft.RecordDir != "" && ui.Button(c.Key("record-dir-default"), "恢复默认").Disabled(v.saving).Clicked() {
							v.draft.RecordDir = ""
						}
					})
					ui.Checkbox(c.Key("ask-record-path"), &v.draft.AskRecordPath, "每次录屏和导出 GIF 时询问保存位置").Disabled(v.saving)
				})
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, "打开录屏").Disabled(s.captureBusy).Clicked() {
						s.showVideo()
					}
					if ui.Button(c, "视频转 GIF…").Clicked() {
						s.showGIF()
					}
				})
			} else if v.tab == 4 {
				v.translateSettings(c)
			} else {
				ui.Text(c, "长截图").FontWeight(600)
				ui.Checkbox(c.Key("automatic-scroll"), &v.draft.AutomaticScroll, "长截图自动向下滚动").Disabled(v.saving || !capture.ScrollingSupported())
				ui.Text(c, "长截图需要持续共享所选窗口或显示器；截图快捷键授权与屏幕共享是两个独立权限。").TextColor(c.Theme().TextMuted)
				if v.streamIssue != "" {
					ui.Text(c, v.streamIssue).TextColor(c.Theme().Danger).Selectable()
				} else {
					ui.Text(c, "在鼠标所在显示器框选，松开后滚动页面或拖动滚动条。自动滚动不可用时，可缓慢手动滚动。").FontSize(12).TextColor(c.Theme().TextMuted)
				}
				if !capture.ScrollingSupported() {
					ui.Text(c, "当前系统暂不支持长截图。").FontSize(12).TextColor(c.Theme().TextMuted)
				}
				if ui.Button(c, "打开长截图").Disabled(s.captureBusy || !capture.ScrollingSupported()).Clicked() {
					s.showScroll()
				}

				if runtime.GOOS == "linux" {
					ui.Text(c, "默认手动滚动；每块显示器首次使用时请选择对应共享来源，之后分别记住。自动模式需要额外的鼠标控制授权。").FontSize(12).TextColor(c.Theme().TextMuted)
					if ui.Button(c, "更换共享来源").Disabled(s.captureBusy || v.saving).Clicked() {
						v.app.worker(func() {
							err := capture.ForgetScrollSource()
							v.later(func() {
								if err != nil {
									v.notice = err.Error()
									return
								}
								if s.scroll != nil {
									s.scroll.start()
								} else {
									s.showScroll()
								}
							})
						})
					}
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
								hint = "已拒绝，可在系统的应用设置中允许 " + brand.Name + " 截图。"
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
			if ui.PrimaryButton(c.Key("save"), label).Disabled(v.saving || s.shortcutPending || v.recording).Clicked() {
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
	if v.saving || v.app.shortcutPending || v.recording {
		return
	}
	if err := v.draft.Validate(); err != nil {
		v.notice = err.Error()
		return
	}
	if niribind.Current() && niribind.Installed() && (v.draft.CaptureShortcut != v.app.cfg.CaptureShortcut || v.draft.ScrollShortcut != v.app.cfg.ScrollShortcut || v.draft.RecordShortcut != v.app.cfg.RecordShortcut) {
		v.setupNiri()
		return
	}
	v.recording = false
	v.saving = true
	v.notice = ""
	provider, id, key := translate.Provider(v.draft.TranslateProvider), v.trID, v.trKey
	if provider != translate.None && (strings.TrimSpace(id) != "" || strings.TrimSpace(key) != "") {
		// Store the keys first, off the main thread: the credential store
		// may ask the user to unlock it.
		v.app.worker(func() {
			err := storeCredentials(provider, id, key)
			v.later(func() {
				if err != nil {
					v.saving = false
					v.notice = "保存翻译密钥失败：" + err.Error()
					return
				}
				v.trID, v.trKey = "", ""
				if v.trStored == nil {
					v.trStored = map[string]bool{}
				}
				v.trStored[string(provider)] = true
				v.saving = false
				v.save()
			})
		})
		return
	}
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
	if v.saving || v.app.shortcutPending || v.recording {
		return
	}
	next := v.draft
	if err := next.Validate(); err != nil {
		v.notice = err.Error()
		return
	}
	v.recording, v.saving, v.confirming, v.notice = false, true, true, ""
	v.app.worker(func() {
		plan, err := niribind.PrepareShortcuts(next.CaptureShortcut, next.ScrollShortcut, next.RecordShortcut)
		if err != nil {
			v.later(func() { v.saving, v.confirming = false, false; v.notice = err.Error() })
			return
		}
		if v.ctx.Err() != nil {
			return
		}
		result, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: v.win, Type: mygo.MessageQuestion, Title: "配置 niri 截图快捷键",
			Message: "将 " + config.DisplayShortcut(plan.Key) + " 设置为截图快捷键，长截图设置为 " + config.DisplayShortcut(plan.ScrollKey) + "，录屏设置为 " + config.DisplayShortcut(plan.RecordKey) + "（空白为不绑定）？",
			Detail:  "这会在 niri 配置中添加一条 include，并创建 " + brand.Name + " 管理的按键文件。若该组合已有操作，将由 " + brand.Name + " 截图替换；更换按键会恢复原操作。原配置会先备份，取消不做修改。\n\n配置：" + plan.ConfigPath + "\n按键文件：" + plan.ShortcutPath + "\n执行：" + plan.Command + " --capture",
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

func (v *settingsWindow) configureShortcuts() {
	if v.saving || v.app.shortcutPending {
		return
	}
	v.saving = true
	v.notice = "请在系统弹窗中设置快捷键；关闭弹窗后查看上方实际按键。"
	v.app.worker(func() {
		err := v.app.shortcuts.Configure(v.ctx)
		v.later(func() {
			v.saving = false
			if err != nil {
				v.notice = err.Error()
			}
		})
	})
}

// chooseRecordDir only changes the draft; "保存设置" applies it.
func (v *settingsWindow) chooseRecordDir() {
	current := v.app.recordDirFor(v.draft.RecordDir)
	v.app.worker(func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: v.win, Title: "选择录屏保存位置", DefaultPath: current, Directory: true, CreateDirectories: true})
		v.later(func() {
			if err != nil {
				v.notice = "选择文件夹失败：" + err.Error()
			} else if len(paths) > 0 {
				v.draft.RecordDir = paths[0]
				v.notice = "点击“保存设置”后生效。"
			}
		})
	})
}
