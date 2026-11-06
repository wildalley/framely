package editor

import (
	"fmt"
	"math"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/config"
	"github.com/wildalley/screenpin/internal/doc"
	"github.com/wildalley/screenpin/internal/render"
)

// settingsState is the settings panel's draft, saved with its button.
type settingsState struct {
	shortcut      string
	hideAfterCopy bool
	recording     bool
	autostart     bool
	access        capture.Access
	asking        bool
	saving        bool
}

func (a *App) openSettings() {
	a.settings = settingsState{
		shortcut:      a.cfg.CaptureShortcut,
		hideAfterCopy: a.cfg.HideAfterCopy,
		autostart:     mygo.App.OpenAtLogin(),
	}
	if runtime.GOOS == "linux" {
		go func() {
			acc := capture.ScreenshotAccess()
			a.later(func() { a.settings.access = acc })
		}()
	}
}

// AutostartChanged follows a change of the tray's checkbox.
func (a *App) AutostartChanged(on bool) { a.settings.autostart = on }

func (a *App) panels(c *ui.Context, barH float32) {
	if a.panel == PanelNone {
		return
	}
	style := a.activeStyle()
	if a.panel == PanelStyle && style == nil {
		a.panel = PanelNone
		return
	}
	area := a.workArea()
	panel := ui.Column(c.Key("panel")).Absolute().Right(8).Bottom(barH+6).
		Width(float32(panelWidth(a.panel))).Padding(11, 12, 12, 12).Gap(9).
		Background(ui.RGBA(250, 250, 248, 0.98)).Border(1, colLine).Radius(7).
		Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.18)).TextColor(colInk).FontSize(13).
		MaxHeight(float32(max(160, area.Height-int(barH)-40))).Clip()
	// Presses on the panel stay in it rather than reaching the canvas.
	panel.HandleInput(func(ev ui.InputEvent) bool {
		return ev.Kind == ui.InputPointerDown || ev.Kind == ui.InputPointerUp
	})
	if h := panel.Bounds().H; h > 0 && math.Abs(float64(h-a.panelHeight)) > 0.5 {
		a.panelHeight = h
		a.win.Update(func() { a.fitWindow(false) })
	}
	panel.Children(func() {
		ui.Scroll(c).Gap(9).Children(func() {
			switch a.panel {
			case PanelStyle:
				a.stylePanel(c, *style)
			case PanelMenu:
				a.menuPanel(c)
			case PanelShortcuts:
				a.shortcutsPanel(c)
			case PanelSettings:
				a.settingsPanel(c)
			}
		})
	})
}

func panelTitle(c *ui.Context, title, hint string) {
	ui.Row(c).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontWeight(650).FontSize(13)
		if hint != "" {
			ui.Text(c, hint).FontSize(12).TextColor(colMuted)
		}
	})
}

// changeStyle changes the look of the selection, and the preset of its
// tool, or else of the active tool. live changes hold the slider's
// transaction instead of making a step each.
func (a *App) changeStyle(change func(*doc.Style), live bool) {
	t := a.styleTarget()
	if t == doc.ToolSelect {
		return
	}
	preset := a.styles[t]
	change(&preset)
	a.styles[t] = preset
	list := a.history.Present()
	i := doc.IndexOf(list, a.selected)
	if i < 0 {
		return
	}
	an := list[i].Clone()
	s := an.Style()
	change(&s)
	an.SetStyle(s)
	next := doc.Replace(list, i, an)
	if live {
		a.history.SetTransient(next)
	} else {
		a.history.Commit(next)
	}
}

// slider is a labeled slider whose drag is one step of the history.
func (a *App) slider(c *ui.Context, key, label string, value, lo, hi float64, suffix string, set func(v float64, live bool)) {
	ui.Column(c).Gap(4).Children(func() {
		ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, label).TextColor(colMuted).FontSize(12)
			ui.Textf(c, "%d%s", int(math.Round(value)), suffix).FontSize(12)
		})
		v := value
		s := ui.StepSlider(c.Key(key), &v, lo, hi, 1).Label(label).FillWidth()
		held := s.Pressed() || s.Dragging()
		if s.Changed() {
			if held {
				if a.slidingKey == "" {
					a.slidingKey = key
					a.history.Begin()
				}
				set(v, true)
			} else {
				set(v, false)
			}
		}
		if a.slidingKey == key && !held {
			a.slidingKey = ""
			a.history.End()
		}
	})
}

func (a *App) stylePanel(c *ui.Context, style doc.Style) {
	target := a.styleTarget()
	sel := a.selectedAnnotation()
	hint := "下次绘制"
	if sel != nil {
		hint = "编辑所选"
	}
	panelTitle(c, toolName(target), hint)
	if target != doc.ToolMosaic {
		ui.Row(c).Gap(6).Wrap().Children(func() {
			for _, col := range doc.Colors {
				b := ui.ButtonBase(c.Key("color-"+col)).Size(22, 22).Radius(11).
					Background(ui.Hex(col)).Border(1, ui.RGBA(0, 0, 0, 0.18)).Label(col)
				if strings.EqualFold(style.Color, col) {
					b.Border(2, colAccent)
				}
				if b.Clicked() {
					a.changeStyle(func(s *doc.Style) { s.Color = col }, false)
				}
			}
		})
	}
	if target != doc.ToolMosaic && target != doc.ToolText {
		hi := 16.0
		if target == doc.ToolHighlighter {
			hi = 40
		}
		a.slider(c, "stroke", "粗细", style.StrokeWidth, 1, hi, "px", func(v float64, live bool) {
			a.changeStyle(func(s *doc.Style) { s.StrokeWidth = v }, live)
		})
	}
	if target == doc.ToolText {
		a.slider(c, "font", "字号", style.FontSize, 12, 96, "px", func(v float64, live bool) {
			a.changeStyle(func(s *doc.Style) { s.FontSize = v }, live)
		})
	}
	if target != doc.ToolMosaic {
		a.slider(c, "opacity", "不透明度", style.Opacity*100, 10, 100, "%", func(v float64, live bool) {
			a.changeStyle(func(s *doc.Style) { s.Opacity = v / 100 }, live)
		})
	}
	if sel != nil && sel.Type == doc.ToolText && a.editingID == "" {
		ui.Text(c, "文字").FontSize(12).TextColor(colMuted)
		text := sel.TextValue()
		field := ui.TextArea(c.Key("style-text"), &text).Lines(2, 6).FillWidth()
		if field.Changed() {
			if !a.textEdit {
				a.textEdit = true
				a.history.Begin()
			}
			list := a.history.Present()
			if i := doc.IndexOf(list, sel.ID); i >= 0 {
				an := list[i].Clone()
				an.SetText(text)
				a.history.SetTransient(doc.Replace(list, i, an))
			}
		}
		if a.textEdit && field.FocusLeft() {
			a.textEdit = false
			a.history.End()
		}
	}
	if sel != nil {
		ui.Row(c).Gap(6).Children(func() {
			if smallButton(c, "layer-down", iconDown, "下移一层", false).Clicked() {
				a.moveLayer(-1)
			}
			if smallButton(c, "layer-up", iconUp, "上移一层", false).Clicked() {
				a.moveLayer(1)
			}
			if smallButton(c, "delete", iconTrash, "删除 (Delete)", true).Clicked() {
				a.removeSelected()
			}
		})
	}
}

func smallButton(c *ui.Context, key string, svg *ui.SVG, label string, danger bool) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Height(30).Grow(1).Radius(5).Center().
		Background(ui.RGBA(255, 255, 255, 1)).Border(1, colLine).Tooltip(label).Label(label)
	if danger {
		b.TextColor(colDanger)
	}
	if b.Hovered() {
		b.Background(colHover)
	}
	b.Children(func() { ui.Icon(c, svg).FontSize(15) })
	return b
}

func menuItem(c *ui.Context, key string, svg *ui.SVG, label string, disabled bool) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Height(32).Padding(0, 8).Radius(5).Gap(8).
		AlignItems(ui.Center).FillWidth().Disabled(disabled)
	if b.Hovered() && !disabled {
		b.Background(colHover)
	}
	b.Children(func() {
		ui.Icon(c, svg).FontSize(15)
		ui.Text(c, label).FontSize(13).SingleLine()
	})
	return b
}

func (a *App) menuPanel(c *ui.Context) {
	ui.Text(c, "名称").FontSize(12).TextColor(colMuted)
	ui.TextInput(c.Key("title"), &a.title).Label("工程名称").FillWidth()
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		if smallButton(c, "zoom-out", iconZoomOut, "缩小 (Ctrl+-)", false).Clicked() {
			a.setZoom(a.displayZoom - 0.1)
		}
		pct := ui.ButtonBase(c.Key("zoom-reset")).Height(30).Grow(1).Radius(5).Center().
			Background(ui.RGBA(255, 255, 255, 1)).Border(1, colLine).Tooltip("100% (Ctrl+0)")
		pct.Children(func() { ui.Textf(c, "%d%%", int(math.Round(a.displayZoom*100))).FontSize(12) })
		if pct.Clicked() {
			a.setZoom(1)
		}
		if smallButton(c, "zoom-in", iconZoomIn, "放大 (Ctrl++)", false).Clicked() {
			a.setZoom(a.displayZoom + 0.1)
		}
		if smallButton(c, "zoom-fit", iconFit, "适应屏幕", false).Clicked() {
			a.zoomToScreen()
		}
	})
	ui.Text(c, "导出格式").FontSize(12).TextColor(colMuted)
	idx := 0
	labels := make([]string, len(render.Formats))
	for i, f := range render.Formats {
		labels[i] = f.Label()
		if f == a.exportFormat {
			idx = i
		}
	}
	if ui.Segmented(c.Key("format"), &idx, labels...).FillWidth().Changed() {
		a.exportFormat = render.Formats[idx]
	}
	if a.exportFormat.Lossy() {
		ui.Column(c).Gap(4).Children(func() {
			ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, "质量").FontSize(12).TextColor(colMuted)
				ui.Textf(c, "%d%%", int(math.Round(a.exportQuality*100))).FontSize(12)
			})
			q := a.exportQuality * 100
			if ui.StepSlider(c.Key("quality"), &q, 40, 100, 1).Label("质量").FillWidth().Changed() {
				a.exportQuality = q / 100
			}
		})
	}
	ui.Column(c).Gap(1).Children(func() {
		if menuItem(c, "m-export", iconDownload, "导出 "+a.exportFormat.Label()+" (Ctrl+S)", false).Clicked() {
			a.Export()
		}
		if menuItem(c, "m-capture", iconCapture, "重新截图", a.busy).Clicked() {
			a.Capture()
		}
		if menuItem(c, "m-paste", iconClipboard, "粘贴剪贴板", a.busy).Clicked() {
			a.Paste()
		}
		if menuItem(c, "m-image", iconImage, "打开图片", false).Clicked() {
			a.OpenImage()
		}
		if menuItem(c, "m-project", iconFolder, "打开工程", false).Clicked() {
			a.OpenProject()
		}
		if menuItem(c, "m-save", iconArchive, "保存工程 (Ctrl+Shift+S)", false).Clicked() {
			a.SaveProject()
		}
	})
	ui.Text(c, fmt.Sprintf("%d × %d", a.image.Width, a.image.Height)).FontSize(12).TextColor(colMuted)
}

func (a *App) shortcutsPanel(c *ui.Context) {
	mod := "Ctrl"
	if runtime.GOOS == "darwin" {
		mod = "Cmd"
	}
	rows := [][2]string{
		{config.DisplayShortcut(a.cfg.CaptureShortcut), "截取屏幕（全局）"},
	}
	for _, it := range toolItems {
		rows = append(rows, [2]string{it.char, toolName(it.tool)})
	}
	rows = append(rows,
		[2]string{mod + "+Z", "撤销"},
		[2]string{mod + "+Shift+Z", "重做"},
		[2]string{mod + "+Y", "重做"},
		[2]string{mod + "+C", "复制到剪贴板"},
		[2]string{mod + "+S", "导出 " + a.exportFormat.Label()},
		[2]string{mod + "+Shift+S", "保存工程"},
		[2]string{mod + "++", "放大"},
		[2]string{mod + "+-", "缩小"},
		[2]string{mod + "+0", "重置缩放"},
		[2]string{mod + "+滚轮", "缩放"},
		[2]string{"Delete", "删除所选"},
		[2]string{"Backspace", "删除所选"},
		[2]string{"Esc", "关闭面板 → 取消选择 → 收回托盘"},
		[2]string{"?", "显示/隐藏快捷键"},
	)
	panelTitle(c, "键盘快捷键", "")
	ui.Column(c).Gap(5).Children(func() {
		for _, r := range rows {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Width(130).Gap(3).Children(func() {
					keys := strings.Split(r[0], "+")
					if strings.HasSuffix(r[0], "++") {
						keys = append(keys[:len(keys)-2], "+")
					}
					for i, k := range keys {
						kbd(c, k)
						if i < len(keys)-1 {
							ui.Text(c, "+").FontSize(11).TextColor(colMuted)
						}
					}
				})
				ui.Text(c, r[1]).FontSize(12).Grow(1)
			})
		}
	})
}

func kbd(c *ui.Context, k string) {
	ui.Box(c).Padding(1, 6).Radius(4).Background(ui.RGBA(255, 255, 255, 1)).Border(1, colLine).Children(func() {
		ui.Text(c, k).FontSize(11).SingleLine()
	})
}

var accessText = map[capture.Access]string{
	capture.AccessGranted: "已授权，截图时直接进入 ScreenPin 选区",
	capture.AccessUnknown: "尚未授权，截图会改用系统的选区界面",
	capture.AccessDenied:  "已被拒绝；可在系统设置的「应用 → ScreenPin」里重新允许截图",
}

func (a *App) settingsPanel(c *ui.Context) {
	s := &a.settings
	panelTitle(c, "设置", "")
	ui.Text(c, "截图快捷键").FontSize(12).TextColor(colMuted)
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Grow(1).Height(30).Padding(0, 8).Radius(5).Border(1, colLine).
			Background(ui.RGBA(255, 255, 255, 1)).Justify(ui.Center).Children(func() {
			ui.Text(c, config.DisplayShortcut(s.shortcut)).FontSize(13)
		})
		label := "录制"
		if s.recording {
			label = "按下快捷键..."
		}
		rec := ui.ButtonBase(c.Key("record")).Height(30).Padding(0, 10).Radius(5).Center().
			Border(1, colLine).Background(ui.RGBA(255, 255, 255, 1))
		if s.recording {
			rec.Background(colSoft).Border(1, colAccent).Focus()
			rec.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind != ui.InputKeyDown {
					return false
				}
				if acc, ok := accelerator(ev.Mods, ev.Key); ok {
					s.shortcut = acc
					s.recording = false
				}
				return true
			})
		}
		rec.Children(func() { ui.Text(c, label).FontSize(13).SingleLine() })
		if rec.Clicked() {
			s.recording = !s.recording
		}
	})
	ui.Text(c, "全局快捷键，在任何界面都可触发截图").FontSize(12).TextColor(colMuted)

	toggle := func(key string, on *bool, title, hint string, disabled bool) bool {
		changed := false
		ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
			changed = ui.Switch(c.Key(key), on).Label(title).Disabled(disabled).Changed()
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, title).FontSize(13)
				ui.Text(c, hint).FontSize(12).TextColor(colMuted)
			})
		})
		return changed
	}
	// Autostart is the system's, not config.json's: it changes at once.
	if toggle("autostart", &s.autostart, "开机自启", "登录后自动在后台启动，随时用快捷键截图（改动立即生效）", false) {
		want := s.autostart
		if err := mygo.App.SetOpenAtLogin(want); err != nil {
			c.Toast(err.Error())
		} else if want {
			c.Toast("已开启开机自启")
		} else {
			c.Toast("已关闭开机自启")
		}
		s.autostart = mygo.App.OpenAtLogin()
		if a.OnAutostartChanged != nil {
			a.OnAutostartChanged(s.autostart)
		}
	}
	if text, ok := accessText[s.access]; ok {
		ui.Text(c, "静默截图").FontSize(12).TextColor(colMuted)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			col := colMuted
			if s.access == capture.AccessGranted {
				col = colAccent
			} else if s.access == capture.AccessDenied {
				col = colDanger
			}
			ui.Text(c, text).FontSize(12).TextColor(col).Grow(1)
			if s.access == capture.AccessUnknown {
				label := "授权"
				if s.asking {
					label = "等待授权…"
				}
				b := ui.Button(c.Key("grant"), label).Disabled(s.asking)
				if b.Clicked() {
					a.askScreenshotAccess()
				}
			}
		})
	}
	toggle("hide-after-copy", &s.hideAfterCopy, "复制后收回托盘", "适合截完就贴；窗口置顶时不收回", false)
	ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c.Key("cancel"), "取消").Clicked() {
			a.panel = PanelNone
		}
		if ui.PrimaryButton(c.Key("save"), "保存").Disabled(s.saving).Clicked() {
			a.saveSettings()
		}
	})
}

// askScreenshotAccess asks for silent screenshots from the settings panel,
// whose click focuses the window, which GNOME needs to ask.
func (a *App) askScreenshotAccess() {
	a.settings.asking = true
	a.win.Focus()
	go func() {
		_, _ = capture.Screenshot(false)
		acc := capture.ScreenshotAccess()
		a.later(func() {
			a.settings.asking = false
			a.settings.access = acc
		})
	}()
}

// saveSettings binds the new shortcut, then writes the configuration; the
// old binding goes only once both worked, and stays if either failed.
func (a *App) saveSettings() {
	s := &a.settings
	next := a.cfg
	next.CaptureShortcut = s.shortcut
	next.HideAfterCopy = s.hideAfterCopy
	if err := config.ValidateShortcut(next.CaptureShortcut); err != nil {
		a.toast(err.Error())
		return
	}
	old := a.cfg.CaptureShortcut
	s.saving = true
	go func() {
		var err error
		if next.CaptureShortcut != old && a.OnShortcutChanged != nil {
			err = a.OnShortcutChanged(old, next.CaptureShortcut)
		}
		if err == nil {
			if err = next.Save(); err != nil && next.CaptureShortcut != old && a.OnShortcutChanged != nil {
				_ = a.OnShortcutChanged(next.CaptureShortcut, old)
			}
		}
		a.later(func() {
			s.saving = false
			if err != nil {
				a.toast(err.Error())
				return
			}
			a.cfg = next
			a.panel = PanelNone
			a.toast("设置已保存")
		})
	}()
}

// accelerator turns a key press into a shortcut in accelerator syntax.
func accelerator(mods ui.Modifiers, key ui.Key) (string, bool) {
	name, ok := keyNames[key]
	if !ok || mods&(ui.Super|ui.Ctrl|ui.Alt) == 0 && !strings.HasPrefix(name, "F") {
		return "", false
	}
	var parts []string
	if mods&ui.Super != 0 {
		parts = append(parts, "Super")
	}
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(append(parts, name), "+"), true
}

var keyNames = func() map[ui.Key]string {
	m := map[ui.Key]string{
		ui.KeySpace: "Space", ui.KeyEnter: "Enter", ui.KeyTab: "Tab", ui.KeyBackspace: "Backspace",
		ui.KeyDelete: "Delete", ui.KeyInsert: "Insert", ui.KeyHome: "Home", ui.KeyEnd: "End",
		ui.KeyPageUp: "PageUp", ui.KeyPageDown: "PageDown", ui.KeyLeft: "Left", ui.KeyRight: "Right",
		ui.KeyUp: "Up", ui.KeyDown: "Down", ui.KeyMinus: "-", ui.KeyEqual: "=", ui.KeyComma: ",",
		ui.KeyPeriod: ".", ui.KeySlash: "/", ui.KeySemicolon: ";", ui.KeyQuote: "'",
		ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]", ui.KeyBackslash: "\\", ui.KeyBackquote: "`",
	}
	letters := []ui.Key{ui.KeyA, ui.KeyB, ui.KeyC, ui.KeyD, ui.KeyE, ui.KeyF, ui.KeyG, ui.KeyH, ui.KeyI,
		ui.KeyJ, ui.KeyK, ui.KeyL, ui.KeyM, ui.KeyN, ui.KeyO, ui.KeyP, ui.KeyQ, ui.KeyR, ui.KeyS, ui.KeyT,
		ui.KeyU, ui.KeyV, ui.KeyW, ui.KeyX, ui.KeyY, ui.KeyZ}
	for i, k := range letters {
		m[k] = string(rune('A' + i))
	}
	digits := []ui.Key{ui.Key0, ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}
	for i, k := range digits {
		m[k] = string(rune('0' + i))
	}
	fkeys := []ui.Key{ui.KeyF1, ui.KeyF2, ui.KeyF3, ui.KeyF4, ui.KeyF5, ui.KeyF6, ui.KeyF7, ui.KeyF8,
		ui.KeyF9, ui.KeyF10, ui.KeyF11, ui.KeyF12}
	for i, k := range fkeys {
		m[k] = fmt.Sprintf("F%d", i+1)
	}
	return m
}()
