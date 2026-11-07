package desktop

import (
	"context"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

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
