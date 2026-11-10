package desktop

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/config"
)

func (v *settingsWindow) shortcutEntry(c *ui.Context, scroll, video bool) {
	value, defaultKey, fieldLabel, recordLabel, defaultLabel := &v.draft.CaptureShortcut, config.DefaultShortcut, "快捷键组合", "录制快捷键", "恢复默认快捷键"
	if scroll {
		value, defaultKey, fieldLabel, recordLabel, defaultLabel = &v.draft.ScrollShortcut, config.DefaultScrollShortcut, "长截图快捷键组合", "录制长截图快捷键", "使用推荐快捷键"
	}
	if video {
		value, defaultKey, fieldLabel, recordLabel, defaultLabel = &v.draft.RecordShortcut, config.DefaultRecordShortcut, "录屏快捷键组合", "录制录屏快捷键", "使用推荐录屏快捷键"
	}
	recording := v.recording && v.recordingScroll == scroll && v.recordingVideo == video
	busy := v.saving || v.app.shortcutPending
	ui.Row(c).Gap(6).AlignItems(ui.Center).MinHeight(28).Children(func() {
		if recording {
			ui.Text(c, "正在录制").FontSize(12).TextColor(c.Theme().TextMuted)
			for _, modifier := range modifierNames(v.recordingMods) {
				keycap(c, keyAppearance(modifier, runtime.GOOS))
			}
			ui.Text(c, "…").TextColor(c.Theme().TextMuted)
		} else {
			shortcutCaps(c, *value)
		}
	})
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		field := ui.TextInput(c.Key("shortcut-value"), value).
			Label(fieldLabel).Placeholder(defaultKey).Grow(1).MinWidth(0).
			Disabled(busy || v.recording)
		if field.Changed() {
			v.recordingScroll = scroll
			v.recordingVideo = video
			v.shortcutHint, v.shortcutInvalid = "点击“保存设置”后生效。", false
			if err := v.draft.Validate(); err != nil {
				v.shortcutHint, v.shortcutInvalid = err.Error(), true
			}
		}
		label := recordLabel
		if recording {
			label = "取消录制"
		}
		record := ui.Button(c.Key("record"), label).Disabled(busy)
		if recording && record.FocusLeft() {
			v.stopRecording()
		}
		if record.Clicked() {
			if recording {
				v.stopRecording()
			} else {
				v.recording, v.recordingMods, v.recordingScroll = true, 0, scroll
				v.recordingVideo = video
				recording = true
				v.shortcutHint, v.shortcutInvalid = "", false
				record.Focus()
			}
		}
		if v.recording && v.recordingScroll == scroll && v.recordingVideo == video {
			record.HandleInput(v.recordShortcut)
		}
	})
	if v.recording && v.recordingScroll == scroll && v.recordingVideo == video {
		preview := strings.Join(modifierNames(v.recordingMods), "+")
		if preview != "" {
			preview += "+"
		}
		ui.Text(c, "正在录制："+config.DisplayShortcut(preview)+"…（Esc 取消）").FontSize(12)
	}
	if v.shortcutHint != "" && v.recordingScroll == scroll && v.recordingVideo == video {
		color := c.Theme().TextMuted
		if v.shortcutInvalid {
			color = c.Theme().Danger
		}
		ui.Text(c, v.shortcutHint).FontSize(12).TextColor(color)
	}
	if ui.Button(c.Key("shortcut-default"), defaultLabel).Disabled(busy).Clicked() {
		v.recordingScroll = scroll
		v.recordingVideo = video
		v.setRecordedShortcut(defaultKey)
	}
	ui.Text(c, "可直接填写组合键；系统占用的按键可能无法录制。推荐："+
		config.DisplayShortcut(defaultKey)+"。").FontSize(12).TextColor(c.Theme().TextMuted)
}

func (v *settingsWindow) stopRecording() {
	if v.recording {
		v.recording, v.recordingMods = false, 0
		v.shortcutHint, v.shortcutInvalid = "已取消录制，原组合键未改动。", false
	}
}

func (v *settingsWindow) setRecordedShortcut(key string) {
	if v.recordingVideo {
		v.draft.RecordShortcut = key
	} else if v.recordingScroll {
		v.draft.ScrollShortcut = key
	} else {
		v.draft.CaptureShortcut = key
	}
	v.recording, v.recordingMods = false, 0
	v.shortcutHint, v.shortcutInvalid = "已录入 "+config.DisplayShortcut(key)+"，点击“保存设置”后生效。", false
	if err := v.draft.Validate(); err != nil {
		v.shortcutHint, v.shortcutInvalid = err.Error(), true
	}
}

func (v *settingsWindow) recordShortcut(ev ui.InputEvent) bool {
	if !v.recording || ev.Kind != ui.InputKeyDown && ev.Kind != ui.InputKeyUp {
		return false
	}
	v.recordingMods = ev.Mods
	if ev.Kind == ui.InputKeyUp || ev.Repeat {
		return true
	}
	if ev.Key == ui.KeyEscape && ev.Mods == 0 {
		v.stopRecording()
		return true
	}
	if ev.Code >= ui.CodeControlLeft && ev.Code <= ui.CodeSuperRight {
		return true // A modifier previews the chord; it does not finish it.
	}
	if key, ok := accelerator(ev); ok {
		v.setRecordedShortcut(key)
	} else {
		v.shortcutHint, v.shortcutInvalid = "请使用至少一个修饰键（Ctrl、Alt、Shift、Super/Cmd）和一个受支持的按键；也可取消后直接填写。", true
	}
	return true
}

// accelerator turns a key press into a shortcut in accelerator syntax.
func accelerator(ev ui.InputEvent) (string, bool) {
	// Codes distinguish the keypad and keys with no ui.Key, such as PrintScreen.
	name, ok := codeNames[ev.Code]
	if !ok {
		key := ev.Key
		if key == ui.KeyUnknown {
			key = ev.PhysicalKey
		}
		name, ok = keyNames[key]
	}
	parts := modifierNames(ev.Mods)
	if !ok || len(parts) == 0 {
		return "", false
	}
	return strings.Join(append(parts, name), "+"), true
}

func modifierNames(mods ui.Modifiers) []string {
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
	return parts
}

var keyNames = func() map[ui.Key]string {
	m := map[ui.Key]string{
		ui.KeyEscape: "Escape",
		ui.KeySpace:  "Space", ui.KeyEnter: "Enter", ui.KeyTab: "Tab", ui.KeyBackspace: "Backspace",
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

var codeNames = func() map[ui.KeyCode]string {
	m := map[ui.KeyCode]string{
		ui.CodePrintScreen: "PrintScreen", ui.CodeCapsLock: "CapsLock",
		ui.CodeNumLock: "NumLock", ui.CodeScrollLock: "ScrollLock",
		ui.CodeNumpad0: "Num0", ui.CodeNumpadDecimal: "NumDec",
		ui.CodeNumpadAdd: "NumAdd", ui.CodeNumpadSubtract: "NumSub",
		ui.CodeNumpadMultiply: "NumMult", ui.CodeNumpadDivide: "NumDiv",
		ui.CodeNumpadEnter: "Enter", ui.CodeNumpadEqual: "=",
		ui.CodeAudioVolumeUp: "VolumeUp", ui.CodeAudioVolumeDown: "VolumeDown",
		ui.CodeAudioVolumeMute: "VolumeMute",
	}
	for code := ui.CodeNumpad1; code <= ui.CodeNumpad9; code++ {
		m[code] = fmt.Sprintf("Num%d", code-ui.CodeNumpad1+1)
	}
	for code := ui.CodeF13; code <= ui.CodeF24; code++ {
		m[code] = fmt.Sprintf("F%d", code-ui.CodeF13+13)
	}
	return m
}()
