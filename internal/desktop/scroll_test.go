package desktop

import (
	"context"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestLongScreenshotPreviewReusesBitmapWithChangingAspectRatio(t *testing.T) {
	v := &scrollWindow{}
	first := image.NewRGBA(image.Rect(0, 0, 720, 400))
	first.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	v.setPreview(first)
	bitmap := v.preview
	second := image.NewRGBA(image.Rect(0, 0, 100, 820))
	for y := 0; y < 820; y++ {
		for x := 0; x < 100; x++ {
			second.SetRGBA(x, y, color.RGBA{G: 255, A: 255})
		}
	}
	v.setPreview(second)
	if v.preview != bitmap || v.previewSize != second.Bounds().Size() {
		t.Fatal("changing preview dimensions created another bitmap")
	}
	w, h := v.preview.Size()
	if w != previewWidth || h != previewHeight {
		t.Fatalf("preview allocation is unbounded: %dx%d", w, h)
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Draw(v.drawPreview)
	}, 200, 200)
	// The new tall preview must retain its aspect and clear the old wide one.
	got := tt.Image()
	if got.RGBAAt(10, 100).G == 255 && got.RGBAAt(10, 100).R == 0 || got.RGBAAt(100, 100).G < 240 || got.RGBAAt(100, 100).R > 20 {
		t.Fatal("preview left pixels outside the contained image")
	}
}

func TestLongScreenshotActionsRemainVisibleAtMinimumWindowSize(t *testing.T) {
	for _, dark := range []bool{false, true} {
		v := &scrollWindow{app: &application{ctx: context.Background()}, running: true, paused: true, canAuto: true, autoGranted: true, canUndo: true, height: 12000, notice: strings.Repeat("页面持续变化，已暂停。", 8)}
		tt := ui.NewTester(v.view, 380, 380)
		tt.SetDark(dark)
		for _, label := range []string{"取消", "撤回最后一段", "继续截图", "完成截图"} {
			r, ok := tt.Find(label)
			if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 380.5 || r.Y+r.H > 380.5 {
				t.Fatalf("dark=%v action=%s bounds=%+v found=%v", dark, label, r, ok)
			}
		}
	}
}
func TestRecordingDuringLongCaptureDoesNotFinishIt(t *testing.T) {
	v := &scrollWindow{running: true}
	s := &application{ctx: context.Background(), scroll: v, settings: &settingsWindow{recording: true}}
	s.CaptureWithActivation("recorded")
	if v.ending {
		t.Fatal("recording a shortcut finished the long screenshot")
	}
}
