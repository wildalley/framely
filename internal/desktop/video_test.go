package desktop

import (
	"context"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/capture"
	"github.com/wildalley/screenpin/internal/config"
)

func TestVideoControlsAtMinimumSize(t *testing.T) {
	for _, size := range [][2]int{{400, 340}, {480, 460}} {
		for _, dark := range []bool{false, true} {
			for _, state := range []string{"ready", "running", "paused", "saved"} {
				t.Run(fmt.Sprintf("%dx%d-%s-dark-%t", size[0], size[1], state, dark), func(t *testing.T) {
					v := &videoWindow{app: &application{ctx: context.Background()}, ctx: context.Background(), fps: "60", quality: "1080p", gifFPS: "15", gifWidth: "960", gifStart: "0", gifDuration: "10"}
					v.running = state == "running" || state == "paused"
					v.paused = state == "paused"
					if state != "ready" {
						v.status = capture.RecordingStatus{Active: v.running, Paused: v.paused, Finished: state == "saved", Width: 1920, Height: 1080, FPS: 60, Encoder: "mpeg4", ElapsedMS: 10000, Frames: 600, Captured: 580, Repeated: 20}
					}
					if state == "saved" {
						v.path = "/home/user/Videos/录屏 2026-11-10.mp4"
					}
					tt := ui.NewTester(v.view, size[0], size[1])
					tt.SetDark(dark)
					button := "选择区域并开始…"
					if v.running {
						button = "结束并保存"
					}
					r, ok := tt.Find(button)
					if !ok || r.Y+r.H > float32(size[1])+.5 || r.X+r.W > float32(size[0])+.5 {
						t.Fatalf("save/start control hidden: %+v", r)
					}
					if state == "saved" {
						tt.Scroll(250, 200, 0, 600)
						tt.Frame()
						if _, ok := tt.Find("导出 GIF…"); !ok {
							t.Fatal("GIF export unreachable")
						}
					}
					saveShortcutPreview(t, tt, fmt.Sprintf("video-%dx%d-%s-dark-%t", size[0], size[1], state, dark))
				})
			}
		}
	}
}

func TestVideoShortcutIsRecordedIndependently(t *testing.T) {
	v := newShortcutSettings()
	tt := ui.NewTester(v.view, 660, 1100)
	if err := tt.Click("录制录屏快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Super|ui.Shift, ui.KeyR)
	if v.recording || v.draft.RecordShortcut != config.DefaultRecordShortcut || v.draft.CaptureShortcut != "Ctrl+Alt+A" {
		t.Fatalf("record action: %+v", v.draft)
	}
}
