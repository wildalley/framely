package desktop

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/recording"
)

func TestSettingsValidationLeadsToFieldAndClearsAfterCorrection(t *testing.T) {
	v := &settingsWindow{app: &application{ctx: context.Background()}, ctx: context.Background(), draft: config.Default(), trStored: map[string]bool{}}
	v.draft.TranslateProvider, v.draft.TranslateBaseURL = "openai", "http://remote.example/v1"
	v.save()
	if v.saving || v.tab != 4 {
		t.Fatal("invalid endpoint did not lead to translation settings")
	}
	tt := ui.NewTester(v.view, 660, 760)
	r, ok := tt.Find("接口地址错误")
	input, _ := tt.Find("接口地址")
	if !ok || r.W <= 0 || r.Y < input.Y+input.H || r.Y-input.Y > 100 {
		t.Fatalf("error is not next to input: %+v / %+v", r, input)
	}
	saveShortcutPreview(t, tt, "settings-field-error")
	v.draft.TranslateBaseURL = "https://remote.example/v1"
	tt.Frame()
	if _, ok := tt.Find("接口地址错误"); ok {
		t.Fatal("corrected endpoint kept stale validation")
	}
}

func TestRecordingRecoveryOffersWorkingPathsAndOpenErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "保留片段.mp4")
	err := fmt.Errorf("save failed: %w", &recording.RecoveryError{Path: path, Err: errors.New("disk full")})
	v := newVideoWindow(&application{ctx: context.Background()}, context.Background(), func() {})
	v.notice, v.recoveryPath = recordingFailure(err)
	var opened []string
	v.openPath = func(p string) error { opened = append(opened, p); return nil }
	tt := ui.NewTester(v.view, 480, 1000)
	for _, label := range []string{"打开保留片段", "打开片段文件夹"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if len(opened) != 2 || opened[0] != path || opened[1] != filepath.Dir(path) {
		t.Fatal(opened)
	}
	v.openPath = func(string) error { return errors.New("no handler") }
	if err := tt.Click("打开保留片段"); err != nil {
		t.Fatal(err)
	}
	if v.diagnostic != "no handler" || v.recoveryPath != path || v.notice == "" {
		t.Fatal("open failure lost artifact or feedback")
	}
}

func TestGIFTimelineKeepsInputsRangeAndExportReachable(t *testing.T) {
	v := newVideoWindow(&application{ctx: context.Background()}, context.Background(), func() {})
	v.tab = videoTabGIF
	v.useGIFSource("/tmp/clip.mp4", 100)
	preview := image.NewRGBA(image.Rect(0, 0, 640, 360))
	for y := 0; y < 360; y++ {
		for x := 0; x < 640; x++ {
			preview.SetRGBA(x, y, color.RGBA{uint8(x / 3), uint8(y / 2), 120, 255})
		}
	}
	v.gifFrame.bitmap = ui.NewBitmap(preview)
	tt := ui.NewTester(v.view, 480, 760)
	r, ok := tt.Find("GIF 选段")
	if !ok || r.W <= 0 {
		t.Fatal("no visual range control")
	}
	tt.Press(r.X+r.W*.03, r.Y+r.H/2)
	tt.Move(r.X+r.W*.07, r.Y+r.H/2)
	tt.Release(r.X+r.W*.07, r.Y+r.H/2)
	if v.gifLow <= 0 {
		t.Fatal("start handle did not move")
	}
	start, duration, valid := v.gifRange()
	if !valid || start != v.gifLow || math.Abs(duration-(v.gifHigh-v.gifLow)) > .00001 {
		t.Fatalf("range/input mismatch: %v %v low=%v high=%v", start, duration, v.gifLow, v.gifHigh)
	}
	if err := tt.Click("maximum"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnd)
	if v.gifHigh-v.gifLow > 60.001 || v.gifHigh != 100 {
		t.Fatal("keyboard endpoint bypassed 60 second limit")
	}
	saveShortcutPreview(t, tt, "gif-timeline-wide")
	tt.SetDark(true)
	saveShortcutPreview(t, tt, "gif-timeline-wide-dark")
	tt.SetDark(false)
	tt.SetSize(400, 360)
	r, ok = tt.Find("导出 GIF")
	if !ok || r.W <= 0 || r.H <= 0 || r.Y+r.H > 360 {
		t.Fatalf("export unreachable: %+v", r)
	}
	saveShortcutPreview(t, tt, "gif-timeline-narrow")
	v.gifStart, v.gifDuration = "NaN", "Inf"
	v.exportGIF()
	if v.exporting || v.gifErrors["start"] == "" || v.gifErrors["duration"] == "" {
		t.Fatal("nonfinite range accepted or no field feedback")
	}
}

func TestGIFPreviewDebouncesCancelsAndDropsLateFrames(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &application{ctx: ctx}
	v := newVideoWindow(s, ctx, cancel)
	started := make(chan context.Context, 3)
	results := make(chan func(), 3)
	allow := make(chan struct{})
	v.gifFrame.run = s.worker
	v.gifFrame.later = func(fn func()) { results <- fn }
	v.gifFrame.load = func(ctx context.Context, _ string, at float64) (image.Image, error) {
		started <- ctx
		if at == 1 {
			<-ctx.Done()
		} else {
			<-allow
		}
		return image.NewRGBA(image.Rect(0, 0, 10, 10)), nil // deliberately late success
	}
	v.useGIFSource("/tmp/first.mp4", 10)
	v.requestGIFPreview(1)
	var old context.Context
	select {
	case old = <-started:
	case <-time.After(time.Second):
		t.Fatal("preview did not start")
	}
	v.requestGIFPreview(2)
	select {
	case <-old.Done():
	case <-time.After(time.Second):
		t.Fatal("superseded media process was not canceled")
	}
	select {
	case fn := <-results:
		fn()
	case <-time.After(time.Second):
		t.Fatal("late callback not queued")
	}
	if v.gifFrame.bitmap != nil || !v.gifFrame.busy {
		t.Fatal("late result replaced current preview")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("current preview did not start")
	}
	close(allow)
	select {
	case fn := <-results:
		fn()
	case <-time.After(time.Second):
		t.Fatal("preview not delivered")
	}
	if v.gifFrame.bitmap == nil || v.gifFrame.at != 2 || v.gifFrame.busy {
		t.Fatal("current preview was not accepted")
	}
	// Replacement and close invalidate even already queued frames.
	v.requestGIFPreview(3)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("next preview did not start")
	}
	select {
	case fn := <-results:
		v.useGIFSource("/tmp/second.mp4", 10)
		v.closed = true
		v.cancelGIFPreview()
		fn()
	case <-time.After(time.Second):
		t.Fatal("next preview not queued")
	}
	s.workers.Wait()
	if v.gifFrame.bitmap != nil {
		t.Fatal("closed/replaced source accepted queued preview")
	}
}
