package desktop

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"runtime"
	"strings"
	"testing"
	"weak"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/capture"
	"github.com/wildalley/framely/internal/portal"
	"github.com/wildalley/framely/internal/scrollshot"
)

func TestCancelledLongScreenshotDoesNotRetainAController(t *testing.T) {
	for _, err := range []error{capture.ErrCancelled, portal.ErrCancelled, context.Canceled, fmt.Errorf("picker: %w", capture.ErrCancelled)} {
		// Repeat cancellation while the coordinator stays alive, as when
		// the user cancels several shots before taking the next one.
		s := &application{ctx: context.Background(), settings: &settingsWindow{}}
		for i := 0; i < 5; i++ {
			ctx, cancel := context.WithCancel(s.ctx)
			v := &scrollWindow{app: s, ctx: ctx, cancel: cancel, busy: true, holdsCapture: true, preview: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 20, 20)))}
			s.scroll, s.captureBusy, s.operations = v, true, 1
			v.applyStartFailure(err)
			v.applyStartFailure(err)
			if !v.closed || v.ctx.Err() == nil || v.preview != nil || s.scroll != nil || s.captureBusy || s.operations != 0 {
				t.Fatalf("cancelled shot %d remained restorable: closed=%t scroll=%v busy=%t operations=%d", i, v.closed, s.scroll, s.captureBusy, s.operations)
			}
		}
	}
}

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

func BenchmarkLongScreenshotPreviewUpdate(b *testing.B) {
	v := &scrollWindow{}
	preview := image.NewRGBA(image.Rect(0, 0, previewWidth, previewHeight))
	v.setPreview(preview)
	v.setPreview(preview)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.setPreview(preview)
	}
}

func TestLongScreenshotPreviewBuffersAreReleasedAfterClose(t *testing.T) {
	s := &application{ctx: context.Background(), settings: &settingsWindow{}}
	ctx, cancel := context.WithCancel(s.ctx)
	v := &scrollWindow{app: s, ctx: ctx, cancel: cancel}
	s.scroll, s.operations = v, 1
	frame := image.NewRGBA(image.Rect(0, 0, 300, 200))
	for i := 0; i < 80; i++ {
		v.setPreview(frame)
	}
	pointers := [2]weak.Pointer[uint8]{weak.Make(&v.previewBuffers[0].Pix[0]), weak.Make(&v.previewBuffers[1].Pix[0])}
	v.release()
	runtime.GC()
	for _, pointer := range pointers {
		if pointer.Value() != nil {
			t.Fatal("closed long screenshot retained a preview buffer")
		}
	}
	runtime.KeepAlive(v)
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

func TestFloatingScrollControlsStayVisibleAndCanPauseAndResume(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, scale := range []float32{1, 1.5, 2} {
			v := &scrollWindow{running: true, floating: true, automatic: true, autoGranted: true, canUndo: true, height: 12000, commands: make(chan scrollshot.Command, 8)}
			tt := ui.NewTester(v.view, 420, 170)
			tt.SetDark(dark)
			tt.SetScale(scale)
			for _, label := range []string{"取消", "撤回", "暂停", "完成", "自动滚动"} {
				r, ok := tt.Find(label)
				if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 420.5 || r.Y+r.H > 170.5 {
					t.Fatalf("dark=%v scale=%g %s: %+v", dark, scale, label, r)
				}
			}
			if err := tt.Click("暂停"); err != nil {
				t.Fatal(err)
			}
			if cmd := <-v.commands; cmd.Kind != "pause" || !v.paused {
				t.Fatal(cmd)
			}
			if err := tt.Click("继续"); err != nil {
				t.Fatal(err)
			}
			if cmd := <-v.commands; cmd.Kind != "resume" || v.paused {
				t.Fatal(cmd)
			}
		}
	}
}

func TestScrollToolbarAvoidsSelectionAndControlsFit(t *testing.T) {
	for _, region := range []ui.Rect{
		{X: 100, Y: 100, W: 600, H: 450},
		{X: 50, Y: 140, W: 1100, H: 550},
		{X: 300, Y: 5, W: 600, H: 770},
	} {
		toolbar, ok := toolbarBeside(region, 1280, 800)
		if !ok {
			t.Fatalf("no controls beside %+v", region)
		}
		if toolbar.X < region.X+region.W && toolbar.X+toolbar.W > region.X && toolbar.Y < region.Y+region.H && toolbar.Y+toolbar.H > region.Y {
			t.Fatal("toolbar contaminates capture")
		}
		if max(toolbar.X-region.X-region.W, region.X-toolbar.X-toolbar.W, toolbar.Y-region.Y-region.H, region.Y-toolbar.Y-toolbar.H) < 32 {
			t.Fatal("compositor shadow can enter selection")
		}
		for _, dark := range []bool{false, true} {
			v := &scrollWindow{running: true, floating: true, overlayRegion: region, overlayToolbar: toolbar, height: 5000, canUndo: true}
			tt := ui.NewTester(v.overlayView, int(toolbar.W), int(toolbar.H))
			tt.SetDark(dark)
			for _, label := range []string{"取消", "撤回", "暂停", "完成"} {
				r, found := tt.Find(label)
				if !found || r.X < 0 || r.Y < 0 || r.X+r.W > toolbar.W+.5 || r.Y+r.H > toolbar.H+.5 {
					t.Fatalf("action=%s dark=%t bounds=%+v toolbar=%+v", label, dark, r, toolbar)
				}
			}
		}
	}
	if _, ok := toolbarBeside(ui.Rect{W: 1280, H: 800}, 1280, 800); ok {
		t.Fatal("full-screen selection silently covered by toolbar")
	}
}

func TestWideScrollSelectionKeepsControlsByTheLowerRightCorner(t *testing.T) {
	// These are real desktop selections whose former 126px panel would
	// jump to the upper left, over two thousand pixels from release.
	for _, region := range []ui.Rect{
		{X: 271, Y: 222, W: 2260, H: 1104},
		{X: 268, Y: 408, W: 2225, H: 920},
		{X: 270, Y: 10, W: 2260, H: 1320},
	} {
		toolbar, ok := toolbarBeside(region, 2560, 1440)
		if !ok || toolbar.W != 420 || toolbar.H != 52 || toolbar.Y != region.Y+region.H+32 || toolbar.X+toolbar.W != min(region.X+region.W, 2560-32) {
			t.Fatalf("toolbar no longer follows the lower right corner: region=%v toolbar=%v", region, toolbar)
		}
	}
}

func TestScrollPreviewBesideAvoidsRegionAndToolbarAndRendersLivePreview(t *testing.T) {
	for _, region := range []ui.Rect{
		{X: 300, Y: 120, W: 800, H: 600},
		{X: 270, Y: 10, W: 2260, H: 1320},
	} {
		toolbar, ok := toolbarBeside(region, 2560, 1440)
		if !ok {
			t.Fatalf("no toolbar for %+v", region)
		}
		previewRect, ok := previewBeside(region, toolbar, 2560, 1440)
		if !ok {
			t.Fatalf("no preview placement for %+v", region)
		}
		if rectsOverlap(previewRect, expandRect(region, 32)) {
			t.Fatalf("preview overlaps capture region: preview=%+v region=%+v", previewRect, region)
		}
		if rectsOverlap(previewRect, expandRect(toolbar, 12)) {
			t.Fatalf("preview overlaps toolbar: preview=%+v toolbar=%+v", previewRect, toolbar)
		}
		for _, dark := range []bool{false, true} {
			v := &scrollWindow{running: true, floating: true, height: 1840}
			sample := image.NewRGBA(image.Rect(0, 0, 240, 480))
			for y := range 480 {
				for x := range 240 {
					sample.SetRGBA(x, y, color.RGBA{R: 40, G: uint8(80 + y/4), B: 140, A: 255})
				}
			}
			v.setPreview(sample)
			tt := ui.NewTester(v.previewOverlayView, int(previewRect.W), int(previewRect.H))
			tt.SetDark(dark)
			for _, label := range []string{"实时预览", "1840 px"} {
				if _, ok := tt.Find(label); !ok {
					t.Fatalf("dark=%v missing %s", dark, label)
				}
			}
			if got := tt.Image().RGBAAt(0, 0); got.A != 0 {
				t.Fatalf("preview corner should be transparent, got %+v", got)
			}
			saveShortcutPreview(t, tt, fmt.Sprintf("scroll-live-preview-dark-%t", dark))
		}
	}
}

func TestCompactScrollStatusShowsModeAndNeutralAlignmentNotice(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		v := &scrollWindow{running: true, automatic: automatic, height: 5000, overlayToolbar: ui.Rect{W: 420, H: 52}}
		tt := ui.NewTester(v.overlayView, 420, 52)
		mode := "手动"
		if automatic {
			mode = "自动"
		}
		if _, ok := tt.Find(mode + " · 5000 px"); !ok {
			t.Fatal("scroll mode is hidden")
		}
		saveShortcutPreview(t, tt, fmt.Sprintf("scroll-mode-auto-%t", automatic))
		v.notice = scrollshot.ErrOverlap.Error()
		tt.Frame()
		if _, ok := tt.Find("等待画面对齐"); !ok || tt.HasText("放慢") {
			t.Fatal("alignment failure still blames scroll speed")
		}
	}
}
