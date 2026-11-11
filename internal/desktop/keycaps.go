package desktop

import (
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/shortcutkey"
)

type shortcutKey struct{ text, name string }

// Tray text uses readable modifier names, while settings render keycaps. Both
// share the same parser for portal descriptions without changing the binding.
func shortcutText(description, platform string) string {
	var chords []string
	for _, chord := range strings.Split(description, " / ") {
		keys, ok := shortcutKeys(chord, platform)
		if !ok {
			chords = append(chords, strings.TrimSpace(chord))
			continue
		}
		var parts []string
		for _, key := range keys {
			part := key.text
			if platform != "darwin" {
				switch key.name {
				case "Shift":
					part = "Shift"
				case "Enter / Return":
					part = "Enter"
				case "Tab":
					part = "Tab"
				}
			}
			parts = append(parts, part)
		}
		chords = append(chords, strings.Join(parts, "+"))
	}
	return strings.Join(chords, " / ")
}

// Display only: portal descriptions are human readable, not accelerators.
// Recognize known forms and preserve everything else. Never save or register
// the result of this conversion as a shortcut.
func shortcutKeys(description, platform string) ([]shortcutKey, bool) {
	value := strings.TrimSpace(strings.TrimSuffix(description, "（niri）"))
	for _, prefix := range []string{"Press", "按"} {
		if rest, ok := strings.CutPrefix(value, prefix); ok && strings.HasPrefix(strings.TrimSpace(rest), "<") {
			value = strings.TrimSpace(rest)
			break
		}
	}
	if strings.HasPrefix(value, "<") {
		var modifiers []string
		for strings.HasPrefix(value, "<") {
			end := strings.IndexByte(value, '>')
			if end < 2 {
				return nil, false
			}
			modifiers = append(modifiers, value[1:end])
			value = value[end+1:]
		}
		value = strings.Join(append(modifiers, value), "+")
	}
	if parsed, err := shortcutkey.Parse(value, platform); err == nil {
		var keys []shortcutKey
		for _, modifier := range []struct {
			set  bool
			name string
		}{{parsed.Super, "Super"}, {parsed.Ctrl, "Ctrl"}, {parsed.Alt, "Alt"}, {parsed.Shift, "Shift"}} {
			if modifier.set {
				keys = append(keys, keyAppearance(modifier.name, platform))
			}
		}
		return append(keys, keyAppearance(parsed.Key, platform)), true
	}
	// Gestures and unfamiliar system descriptions remain text.
	if value == "空格" {
		value = "Space"
	}
	if !strings.Contains(value, "+") {
		if parsed, err := shortcutkey.Parse("Ctrl+"+value, platform); err == nil {
			return []shortcutKey{keyAppearance(parsed.Key, platform)}, true
		}
	}
	return nil, false
}

func keyAppearance(key, platform string) shortcutKey {
	switch key {
	case "Super":
		switch platform {
		case "darwin":
			return shortcutKey{"⌘", "Command"}
		case "windows":
			return shortcutKey{"Win", "Windows"}
		default:
			return shortcutKey{"Super", "Super（通常为 Windows 标志键）"}
		}
	case "Ctrl":
		if platform == "darwin" {
			return shortcutKey{"⌃", "Control"}
		}
		return shortcutKey{"Ctrl", "Control"}
	case "Alt":
		if platform == "darwin" {
			return shortcutKey{"⌥", "Option"}
		}
	case "Shift":
		return shortcutKey{"⇧", "Shift"}
	case "Enter":
		return shortcutKey{"↵", "Enter / Return"}
	case "Escape":
		return shortcutKey{"Esc", "Escape"}
	case "Space":
		return shortcutKey{"Space", "空格"}
	case "Backspace":
		if platform == "darwin" {
			return shortcutKey{"⌫", "Delete（向后删除 / Backspace）"}
		}
	case "Delete":
		if platform == "darwin" {
			return shortcutKey{"⌦", "Forward Delete（向前删除）"}
		}
		return shortcutKey{"Del", "Delete"}
	case "Tab":
		return shortcutKey{"⇥", "Tab"}
	case "Plus":
		return shortcutKey{"+", "+"}
	case "Up":
		return shortcutKey{"↑", "上方向键"}
	case "Down":
		return shortcutKey{"↓", "下方向键"}
	case "Left":
		return shortcutKey{"←", "左方向键"}
	case "Right":
		return shortcutKey{"→", "右方向键"}
	}
	return shortcutKey{key, key}
}

func keycap(c *ui.Context, key shortcutKey) {
	t := c.Theme()
	ui.Text(c, key.text).Label(key.name).Tooltip(key.name).
		FontSize(13).FontWeight(500).TextColor(t.Text).
		MinWidth(28).MinHeight(27).Padding(4, 8).TextAlign(ui.Center).
		Radius(5).Background(t.Background.Mix(t.Surface, .45)).
		Border(1, t.Border).Shadow(0, 1, 1, 0, t.Text.Alpha(.12)).Shrink(0)
}

func shortcutCaps(c *ui.Context, description string) {
	if strings.TrimSpace(description) == "" {
		description = "未绑定"
	}
	ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Grow(1).MinWidth(0).
		Role(ui.RoleGroup).Label(description).Tooltip(description).Children(func() {
		for i, chord := range strings.Split(description, " / ") {
			if i > 0 {
				ui.Text(c, "/").TextColor(c.Theme().TextMuted).FontSize(12)
			}
			if keys, ok := shortcutKeys(chord, runtime.GOOS); ok {
				ui.Row(c).Gap(5).Wrap().MinWidth(0).Children(func() {
					for _, key := range keys {
						keycap(c, key)
					}
				})
			} else {
				ui.Text(c, chord).FontSize(12).TextColor(c.Theme().TextMuted).MinWidth(0).Selectable()
			}
		}
	})
}

func keyRow(c *ui.Context, key, action string) {
	width, _ := c.Size()
	labelWidth := float32(128)
	if width >= 600 {
		labelWidth = 184
	}
	ui.Row(c).Gap(16).AlignItems(ui.Center).MinHeight(30).Children(func() {
		ui.Text(c, action).Width(labelWidth).Shrink(0).FontSize(13)
		shortcutCaps(c, key)
	})
}

func globalKeyRow(c *ui.Context, key, action string) {
	ui.Row(c).Gap(16).AlignItems(ui.Center).MinHeight(32).Label(action + "：" + key).Children(func() {
		ui.Text(c, action).Width(80).Shrink(0).FontWeight(500)
		shortcutCaps(c, key)
	})
}
