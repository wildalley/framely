package shortcutkey

import "testing"

func TestNormalizationAndPortalTriggers(t *testing.T) {
	for _, tc := range []struct{ input, platform, canonical, trigger string }{
		{"Win+Shift+s", "linux", "Super+Shift+S", "LOGO+SHIFT+s"},
		{" shift + control + PageUp ", "linux", "Ctrl+Shift+PageUp", "CTRL+SHIFT+Page_Up"},
		{"CommandOrControl+Space", "darwin", "Super+Space", "LOGO+space"},
		{"CmdOrCtrl+Space", "windows", "Ctrl+Space", "CTRL+space"},
		{"Ctrl++", "linux", "Ctrl+Plus", "CTRL+plus"},
		{"Alt+F24", "linux", "Alt+F24", "ALT+F24"},
		{"Super+ArrowLeft", "linux", "Super+Left", "LOGO+Left"},
		{"Ctrl+Minus", "linux", "Ctrl+-", "CTRL+minus"},
		{"Ctrl+BracketLeft", "linux", "Ctrl+[", "CTRL+bracketleft"},
		{"Alt+MediaPlayPause", "linux", "Alt+MediaPlayPause", "ALT+XF86AudioPlay"},
		{"Ctrl+Num1", "linux", "Ctrl+Num1", "CTRL+KP_1"},
	} {
		t.Run(tc.input+"/"+tc.platform, func(t *testing.T) {
			key, err := Parse(tc.input, tc.platform)
			if err != nil || key.Canonical() != tc.canonical || key.XDGTrigger() != tc.trigger {
				t.Fatalf("key=%+v, err=%v", key, err)
			}
		})
	}
}

func TestRejectsUnsupportedAndDuplicateKeys(t *testing.T) {
	for _, input := range []string{"", "A", "Ctrl", "Ctrl+Shift", "Ctrl+Ctrl+A", "Super+Win+A", "CmdOrCtrl+Ctrl+A", "Ctrl+", "Ctrl+++", "Ctrl+A+B", "Alt+F25", "Alt+F01", "Ctrl+未知", "Ctrl+NotAKey"} {
		if _, err := Parse(input, "linux"); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}
