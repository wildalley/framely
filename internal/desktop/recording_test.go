package desktop

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/config"
)

func TestShortcutRecordingTakesTheFirstChord(t *testing.T) {
	for _, tc := range []struct {
		name  string
		press func(*ui.Tester)
		want  string
	}{
		{"super-shift-s", func(tt *ui.Tester) {
			tt.CodeDown(ui.Super, ui.CodeSuperLeft)
			tt.CodeDown(ui.Super|ui.Shift, ui.CodeShiftLeft)
			tt.Key(ui.Super|ui.Shift, ui.KeyS)
		}, config.DisplayShortcut(config.DefaultShortcut)},
		{"letter", func(tt *ui.Tester) { tt.Key(ui.Ctrl|ui.Shift, ui.KeyA) }, "Ctrl+Shift+A"},
		{"shift-letter", func(tt *ui.Tester) { tt.Key(ui.Shift, ui.KeyA) }, "Shift+A"},
		{"tab", func(tt *ui.Tester) { tt.Key(ui.Ctrl, ui.KeyTab) }, "Ctrl+Tab"},
		{"enter", func(tt *ui.Tester) { tt.Key(ui.Ctrl, ui.KeyEnter) }, "Ctrl+Enter"},
		{"close-window-chord", func(tt *ui.Tester) { tt.Key(ui.Cmd, ui.KeyW) }, config.DisplayShortcut("CmdOrCtrl+W")},
		{"modified-escape", func(tt *ui.Tester) { tt.Key(ui.Ctrl, ui.KeyEscape) }, "Ctrl+Escape"},
		{"print-screen", func(tt *ui.Tester) { tt.CodeDown(ui.Ctrl, ui.CodePrintScreen) }, "Ctrl+PrintScreen"},
		{"keypad", func(tt *ui.Tester) { tt.CodeDown(ui.Ctrl, ui.CodeNumpad1) }, "Ctrl+Num1"},
		{"extended-function-key", func(tt *ui.Tester) { tt.CodeDown(ui.Alt, ui.CodeF24) }, "Alt+F24"},
		{"non-latin-layout", func(tt *ui.Tester) { tt.KeyAt(ui.Ctrl, ui.KeyS) }, "Ctrl+S"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("recording triggered a window action: %v", r)
				}
			}()
			v := newShortcutSettings()
			tt := ui.NewTester(v.view, 660, 640)
			if err := tt.Click("录制快捷键"); err != nil {
				t.Fatal(err)
			}
			tc.press(tt)
			if v.recording || config.DisplayShortcut(v.draft.CaptureShortcut) != tc.want {
				t.Fatalf("first chord: recording=%v shortcut=%q, want %q", v.recording, v.draft.CaptureShortcut, tc.want)
			}
			if err := config.ValidateShortcut(v.draft.CaptureShortcut); err != nil {
				t.Fatalf("recorded an unsaveable chord: %v", err)
			}
		})
	}
}

func newShortcutSettings() *settingsWindow {
	return &settingsWindow{app: &application{ctx: context.Background()}, ctx: context.Background(),
		draft: config.Config{CaptureShortcut: "Ctrl+Alt+A"}}
}

func TestShortcutRecordingCancellationAndFeedback(t *testing.T) {
	for _, exit := range []string{"escape", "button", "tab", "blur"} {
		t.Run(exit, func(t *testing.T) {
			v := newShortcutSettings()
			tt := ui.NewTester(v.view, 660, 640)
			if err := tt.Click("录制快捷键"); err != nil {
				t.Fatal(err)
			}
			tt.CodeDown(ui.Super, ui.CodeSuperRight)
			tt.CodeDown(ui.Super|ui.Shift, ui.CodeShiftRight)
			if !v.recording || !tt.HasText("正在录制："+config.DisplayShortcut("Super+Shift")+"+…（Esc 取消）") {
				t.Fatal("held modifiers were not previewed")
			}
			tt.CodeUp(ui.Super, ui.CodeShiftRight)
			tt.CodeUp(0, ui.CodeSuperRight)
			tt.Key(0, ui.KeyA)
			if !v.recording || !v.shortcutInvalid || v.shortcutHint == "" {
				t.Fatal("invalid chord was silently ignored or ended recording")
			}
			v.save()
			if v.saving {
				t.Fatal("saved an incomplete recording")
			}
			switch exit {
			case "escape":
				tt.Key(0, ui.KeyEscape)
			case "button":
				if err := tt.Click("取消录制"); err != nil {
					t.Fatal(err)
				}
			case "tab":
				if err := tt.Click("常规"); err != nil {
					t.Fatal(err)
				}
			case "blur":
				tt.SetFocused(false)
				tt.SetFocused(true)
			}
			if v.recording || v.draft.CaptureShortcut != "Ctrl+Alt+A" {
				t.Fatalf("cancel changed the draft: recording=%v shortcut=%q", v.recording, v.draft.CaptureShortcut)
			}
		})
	}
}

func TestShortcutManualEntryAndDefaultDoNotNeedAKeyEvent(t *testing.T) {
	v := newShortcutSettings()
	tt := ui.NewTester(v.view, 660, 640)
	if err := tt.Click("快捷键组合"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Super+Shift+S")
	if v.draft.CaptureShortcut != config.DefaultShortcut || v.shortcutInvalid || v.recording || v.saving {
		t.Fatalf("manual entry did not update only the draft: %+v", v)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Super+Shift")
	if !v.shortcutInvalid {
		t.Fatal("invalid manual entry has no inline error")
	}
	if err := tt.Click("恢复默认快捷键"); err != nil {
		t.Fatal(err)
	}
	if v.draft.CaptureShortcut != config.DefaultShortcut || v.shortcutInvalid || v.recording || v.saving {
		t.Fatalf("default did not update only the draft: %+v", v)
	}
}

func TestShortcutRecorderLayout(t *testing.T) {
	for _, size := range [][2]float32{{420, 380}, {660, 640}} {
		for _, dark := range []bool{false, true} {
			name := fmt.Sprintf("%dx%d-dark-%t", int(size[0]), int(size[1]), dark)
			t.Run(name, func(t *testing.T) {
				v := newShortcutSettings()
				tt := ui.NewTester(v.view, int(size[0]), int(size[1]))
				tt.SetDark(dark)
				if err := tt.Click("录制快捷键"); err != nil {
					t.Fatal(err)
				}
				tt.CodeDown(ui.Super|ui.Shift, ui.CodeShiftLeft)
				for _, label := range []string{"快捷键组合", "取消录制", "恢复默认快捷键", "保存设置", "关闭"} {
					r, ok := tt.Find(label)
					if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > size[0]+.5 || r.Y+r.H > size[1]+.5 {
						t.Fatalf("%s is obscured: %+v", label, r)
					}
				}
				if out := os.Getenv("SCREENPIN_UI_SCREENSHOTS"); out != "" {
					if err := os.MkdirAll(out, 0o755); err != nil {
						t.Fatal(err)
					}
					f, err := os.Create(filepath.Join(out, "shortcut-"+name+".png"))
					if err != nil {
						t.Fatal(err)
					}
					defer f.Close()
					if err = png.Encode(f, tt.Image()); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestShortcutAcceleratorKeepsKeypadDistinct(t *testing.T) {
	for _, ev := range []ui.InputEvent{
		{Key: ui.Key1, Code: ui.CodeNumpad1, Mods: ui.Ctrl},
		{Key: ui.KeyEnd, Code: ui.CodeNumpad1, Mods: ui.Ctrl},
	} {
		if got, ok := accelerator(ev); !ok || got != "Ctrl+Num1" {
			t.Fatalf("keypad converted to main keyboard: %q %v", got, ok)
		}
	}
}

func TestScrollShortcutIsRecordedIndependently(t *testing.T) {
	if !capture.ScrollingSupported() {
		t.Skip("long screenshots are unsupported on this platform")
	}
	v := newShortcutSettings()
	tt := ui.NewTester(v.view, 660, 900)
	if err := tt.Click("录制长截图快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Super|ui.Shift, ui.KeyL)
	if v.recording || v.draft.ScrollShortcut != config.DefaultScrollShortcut || v.draft.CaptureShortcut != "Ctrl+Alt+A" {
		t.Fatalf("separate recording: %+v", v.draft)
	}
	if err := tt.Click("长截图快捷键组合"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Ctrl+Alt+A")
	if !v.shortcutInvalid {
		t.Fatal("conflicting chords were accepted")
	}
}

func TestSystemShortcutsShowActualAssignmentsAndConfiguration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("desktop portal configuration")
	}
	v := newShortcutSettings()
	v.app.shortcutSystemManaged = true
	v.app.shortcutDescription = "Press <Shift><Super>a"
	v.app.scrollShortcutDescription = "Super+Shift+L"
	for _, dark := range []bool{false, true} {
		tt := ui.NewTester(v.view, 420, 380)
		tt.SetDark(dark)
		for _, label := range []string{"截图：Press <Shift><Super>a", "长截图：Super+Shift+L", "配置系统快捷键", "保存设置"} {
			r, ok := tt.Find(label)
			if !ok || r.Y+r.H > 380.5 {
				t.Fatalf("actual shortcut/configuration obscured: %s %+v", label, r)
			}
		}
		if _, ok := tt.Find("录制快捷键"); ok {
			t.Fatal("system-managed shortcut shown as an editable local preference")
		}
		if out := os.Getenv("SCREENPIN_UI_SCREENSHOTS"); out != "" {
			if err := os.MkdirAll(out, 0o755); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(out, fmt.Sprintf("system-shortcuts-dark-%t.png", dark)))
			if err != nil {
				t.Fatal(err)
			}
			if err = png.Encode(f, tt.Image()); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
		wide := ui.NewTester(v.view, 660, 640)
		wide.SetDark(dark)
		saveShortcutPreview(t, wide, fmt.Sprintf("settings-660x640-dark-%t", dark))
		wide.Scroll(550, 450, 0, 350)
		saveShortcutPreview(t, wide, fmt.Sprintf("reference-660x640-dark-%t", dark))
	}
}
