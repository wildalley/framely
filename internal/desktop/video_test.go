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
					v := newVideoWindow(&application{ctx: context.Background(), cfg: config.Config{RecordDir: "/home/user/Videos/ScreenPin"}}, context.Background(), func() {})
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
					if state == "ready" {
						if _, ok := tt.Find("/home/user/Videos/ScreenPin"); !ok {
							t.Fatal("default save location hidden")
						}
					}
					if state == "saved" {
						v.useGIFSource(v.path, 10)
						if err := tt.Click("转 GIF"); err != nil {
							t.Fatal(err)
						}
						r, ok := tt.Find("导出 GIF")
						if !ok || r.Y+r.H > float32(size[1])+.5 {
							t.Fatalf("GIF export unreachable: %+v", r)
						}
						saveShortcutPreview(t, tt, fmt.Sprintf("video-gif-%dx%d-dark-%t", size[0], size[1], dark))
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

func TestVideoTrayStatusShowsRecordingNotCapture(t *testing.T) {
	v := newVideoWindow(&application{ctx: context.Background()}, context.Background(), func() {})
	if v.trayStatus() != "" || (*videoWindow)(nil).trayStatus() != "" {
		t.Fatal("idle window reports activity")
	}
	v.busy = true
	if got := v.trayStatus(); got != "正在准备录屏：框选区域…" {
		t.Fatal(got)
	}
	v.busy, v.running = false, true
	v.status.ElapsedMS = 83_400
	if got := v.trayStatus(); got != "● 正在录屏 · 01:23" {
		t.Fatal(got)
	}
	v.paused = true
	if got := v.trayStatus(); got != "录屏已暂停 · 01:23" {
		t.Fatal(got)
	}
	v.ending = true
	if got := v.trayStatus(); got != "正在保存录屏…" {
		t.Fatal(got)
	}
	v.running, v.paused, v.ending, v.exporting, v.gifProgress = false, false, false, true, .426
	if got := v.trayStatus(); got != "正在导出 GIF · 42%" {
		t.Fatal(got)
	}
	if clock(3_723_000) != "1:02:03" {
		t.Fatal(clock(3_723_000))
	}
}

func TestGIFTabValidatesAgainstVideoLength(t *testing.T) {
	v := newVideoWindow(&application{ctx: context.Background()}, context.Background(), func() {})
	v.tab = videoTabGIF
	tt := ui.NewTester(v.view, 400, 360)
	if _, ok := tt.Find("选择视频…"); !ok {
		t.Fatal("video chooser missing")
	}
	v.useGIFSource("/tmp/clip.mp4", 4.5)
	if v.gifDuration != "4.50" || v.gifStart != "0" {
		t.Fatal(v.gifDuration, v.gifStart)
	}
	v.gifStart = "5"
	v.exportGIF()
	if v.exporting || v.notice == "" {
		t.Fatal("start beyond the video accepted")
	}
}
