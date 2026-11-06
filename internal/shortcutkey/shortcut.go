// Package shortcutkey normalizes the accelerators ScreenPin stores and
// translates them to the XDG portal's modifier and XKB keysym names.
package shortcutkey

import (
	"errors"
	"strconv"
	"strings"
)

type Shortcut struct {
	Super, Ctrl, Alt, Shift bool
	Key                     string
	keysym                  string
}

func Parse(s, platform string) (Shortcut, error) {
	var out Shortcut
	invalid := errors.New("快捷键格式无效或包含不受支持的按键")
	if len(s) == 0 || len(s) > 100 {
		return out, invalid
	}
	parts := strings.Split(s, "+")
	if strings.HasSuffix(strings.TrimSpace(s), "++") {
		parts = append(parts[:len(parts)-2], "+")
	}
	if len(parts) < 2 {
		return out, errors.New("快捷键必须包含修饰键和普通按键")
	}
	for _, part := range parts[:len(parts)-1] {
		var target *bool
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "super", "win", "meta", "cmd", "command":
			target = &out.Super
		case "ctrl", "control":
			target = &out.Ctrl
		case "alt", "option":
			target = &out.Alt
		case "shift":
			target = &out.Shift
		case "commandorcontrol", "cmdorctrl":
			if platform == "darwin" {
				target = &out.Super
			} else {
				target = &out.Ctrl
			}
		default:
			return Shortcut{}, invalid
		}
		if *target {
			return Shortcut{}, errors.New("快捷键不能包含重复的修饰键")
		}
		*target = true
	}
	key := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	if len(key) == 1 && ((key[0] >= 'a' && key[0] <= 'z') || (key[0] >= '0' && key[0] <= '9')) {
		out.Key, out.keysym = strings.ToUpper(key), key
		return out, nil
	}
	if strings.HasPrefix(key, "f") {
		if n, err := strconv.Atoi(key[1:]); err == nil && n >= 1 && n <= 24 && key == "f"+strconv.Itoa(n) {
			out.Key, out.keysym = strings.ToUpper(key), strings.ToUpper(key)
			return out, nil
		}
	}
	if len(key) == 4 && strings.HasPrefix(key, "num") && key[3] >= '0' && key[3] <= '9' {
		out.Key, out.keysym = "Num"+key[3:], "KP_"+key[3:]
		return out, nil
	}
	if pair, ok := keys[key]; ok {
		out.Key, out.keysym = pair[0], pair[1]
		return out, nil
	}
	return Shortcut{}, invalid
}

var keys = map[string][2]string{
	"space": {"Space", "space"}, "tab": {"Tab", "Tab"},
	"enter": {"Enter", "Return"}, "return": {"Enter", "Return"},
	"esc": {"Escape", "Escape"}, "escape": {"Escape", "Escape"},
	"backspace": {"Backspace", "BackSpace"}, "delete": {"Delete", "Delete"}, "del": {"Delete", "Delete"},
	"insert": {"Insert", "Insert"}, "home": {"Home", "Home"}, "end": {"End", "End"},
	"pageup": {"PageUp", "Page_Up"}, "pagedown": {"PageDown", "Page_Down"},
	"up": {"Up", "Up"}, "arrowup": {"Up", "Up"}, "down": {"Down", "Down"}, "arrowdown": {"Down", "Down"},
	"left": {"Left", "Left"}, "arrowleft": {"Left", "Left"}, "right": {"Right", "Right"}, "arrowright": {"Right", "Right"},
	"+": {"Plus", "plus"}, "plus": {"Plus", "plus"}, "-": {"-", "minus"}, "minus": {"-", "minus"},
	"=": {"=", "equal"}, "equal": {"=", "equal"},
	",": {",", "comma"}, "comma": {",", "comma"}, ".": {".", "period"}, "period": {".", "period"},
	"/": {"/", "slash"}, "slash": {"/", "slash"}, "\\": {"\\", "backslash"}, "backslash": {"\\", "backslash"},
	";": {";", "semicolon"}, "semicolon": {";", "semicolon"}, "'": {"'", "apostrophe"}, "quote": {"'", "apostrophe"},
	"[": {"[", "bracketleft"}, "bracketleft": {"[", "bracketleft"}, "]": {"]", "bracketright"}, "bracketright": {"]", "bracketright"},
	"`": {"`", "grave"}, "backquote": {"`", "grave"}, "printscreen": {"PrintScreen", "Print"},
	"capslock": {"CapsLock", "Caps_Lock"}, "numlock": {"NumLock", "Num_Lock"}, "scrolllock": {"ScrollLock", "Scroll_Lock"},
	"volumeup": {"VolumeUp", "XF86AudioRaiseVolume"}, "volumedown": {"VolumeDown", "XF86AudioLowerVolume"}, "volumemute": {"VolumeMute", "XF86AudioMute"},
	"medianexttrack": {"MediaNextTrack", "XF86AudioNext"}, "mediaprevioustrack": {"MediaPreviousTrack", "XF86AudioPrev"},
	"mediastop": {"MediaStop", "XF86AudioStop"}, "mediaplaypause": {"MediaPlayPause", "XF86AudioPlay"},
	"numdec": {"NumDec", "KP_Decimal"}, "numadd": {"NumAdd", "KP_Add"}, "numsub": {"NumSub", "KP_Subtract"},
	"nummult": {"NumMult", "KP_Multiply"}, "numdiv": {"NumDiv", "KP_Divide"},
}

func (s Shortcut) Canonical() string {
	return strings.Join(s.parts("Super", "Ctrl", "Alt", "Shift", s.Key), "+")
}

func (s Shortcut) XDGTrigger() string {
	return strings.Join(s.parts("LOGO", "CTRL", "ALT", "SHIFT", s.keysym), "+")
}

func (s Shortcut) parts(super, ctrl, alt, shift, key string) []string {
	var parts []string
	for _, part := range []struct {
		active bool
		name   string
	}{{s.Super, super}, {s.Ctrl, ctrl}, {s.Alt, alt}, {s.Shift, shift}} {
		if part.active {
			parts = append(parts, part.name)
		}
	}
	return append(parts, key)
}
