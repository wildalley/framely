package desktop

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestShortcutKeycapsRespectPlatformAndLiteralKeys(t *testing.T) {
	for _, tc := range []struct{ input, platform, want string }{
		{"Super+Shift+S", "windows", "Win ⇧ S"},
		{"Super+Shift+S", "linux", "Super ⇧ S"},
		{"Super+Shift+S", "darwin", "⌘ ⇧ S"},
		{"Ctrl+Alt+Shift+S", "darwin", "⌃ ⌥ ⇧ S"},
		{"CommandOrControl+S", "darwin", "⌘ S"},
		{"CommandOrControl+S", "windows", "Ctrl S"},
		{"CommandOrControl+S", "linux", "Ctrl S"},
		{"按 <Shift><Super>s", "linux", "Super ⇧ S"},
		{"Press <Control><Alt>Return", "linux", "Ctrl Alt ↵"},
		{"Win+Shift+L（niri）", "linux", "Super ⇧ L"},
		{"Ctrl++", "linux", "Ctrl +"},
		{"Ctrl+/", "windows", "Ctrl /"},
		{"Ctrl+Num1", "windows", "Ctrl Num1"},
		{"Ctrl+1", "windows", "Ctrl 1"},
		{"Backspace", "darwin", "⌫"},
		{"Delete", "darwin", "⌦"},
		{"Delete", "windows", "Del"},
		{"空格", "linux", "Space"},
	} {
		keys, ok := shortcutKeys(tc.input, tc.platform)
		var labels []string
		for _, key := range keys {
			labels = append(labels, key.text)
		}
		if !ok || strings.Join(labels, " ") != tc.want {
			t.Errorf("%s/%s: %+v, want %s", tc.platform, tc.input, keys, tc.want)
		}
	}
	for _, description := range []string{"", "未绑定", "注册中…", "Double tap Super", "按 <Unknown>s", "按 <Super", "Ctrl+滚轮", "拖动"} {
		if _, ok := shortcutKeys(description, "linux"); ok {
			t.Errorf("invented keycaps for system description %q", description)
		}
	}
}

func TestTrayShortcutTextUsesActualAssignmentWithoutPortalSyntax(t *testing.T) {
	for _, tc := range []struct{ input, platform, want string }{
		{"按<shift><super>s", "linux", "Super+Shift+S"},
		{"按 <Shift><Super>s", "linux", "Super+Shift+S"},
		{"Press <Control><Alt>Return", "linux", "Ctrl+Alt+Enter"},
		{"Super+Shift+R", "windows", "Win+Shift+R"},
		{"Super+Shift+R", "darwin", "⌘+⇧+R"},
		{"Super+Shift+L（niri）", "linux", "Super+Shift+L"},
		{"Double tap Super", "linux", "Double tap Super"},
		{"按 <Unknown>s", "linux", "按 <Unknown>s"},
		{"Ctrl+1 / Ctrl+Num1", "linux", "Ctrl+1 / Ctrl+Num1"},
	} {
		if got := shortcutText(tc.input, tc.platform); got != tc.want {
			t.Errorf("%q/%s: %q, want %q", tc.input, tc.platform, got, tc.want)
		}
	}
}

func TestShortcutKeycapsKeepSymbolsNamedAndLongChordsVisible(t *testing.T) {
	for _, platform := range []string{"windows", "darwin", "linux"} {
		for _, dark := range []bool{false, true} {
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
					ui.Text(c, platform).FontWeight(600)
					for _, value := range []string{"Super+Shift+S", "Ctrl+Alt+Shift+PrintScreen", "CommandOrControl+Enter", "Backspace", "Delete"} {
						ui.Row(c).Gap(5).Wrap().Children(func() {
							keys, _ := shortcutKeys(value, platform)
							for _, key := range keys {
								keycap(c, key)
							}
						})
					}
				})
			}, 360, 300)
			tt.SetDark(dark)
			for _, label := range []string{"Shift", "Control", "PrintScreen", "Enter / Return"} {
				bounds, ok := tt.Find(label)
				if !ok || bounds.X < 0 || bounds.X+bounds.W > 360.5 || bounds.Y+bounds.H > 300.5 {
					t.Fatalf("%s dark=%v %s obscured: %+v", platform, dark, label, bounds)
				}
				if _, ok := tt.Access(label); !ok {
					t.Fatalf("symbol has no readable name: %s %s", platform, label)
				}
			}
			saveShortcutPreview(t, tt, fmt.Sprintf("keycaps-%s-dark-%t", platform, dark))
		}
	}
}

func saveShortcutPreview(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	if out := os.Getenv("SCREENPIN_UI_SCREENSHOTS"); out != "" {
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(out, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
}
