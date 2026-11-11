package desktop

import (
	"image"
	"os"
	"runtime"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/screenpin/internal/editor"
)

func testRecordingSelection() *editor.ScreenSelection {
	return &editor.ScreenSelection{
		Display: mygo.Rectangle{X: -1920, Width: 1920, Height: 1080},
		Region:  image.Rect(100, 100, 900, 700), Size: image.Pt(1920, 1080),
	}
}

func TestRecordingSetupSurvivesRestartAndRejectsInvalidHistory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG configuration fixture")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	setup := recordingSetup{Selection: testRecordingSelection(), FPS: "30", Quality: "1440p"}
	if err := saveRecordingSetup(setup); err != nil {
		t.Fatal(err)
	}
	loaded := loadRecordingSetup()
	if loaded.Selection == nil || *loaded.Selection != *setup.Selection || loaded.FPS != "30" || loaded.Quality != "1440p" {
		t.Fatalf("lost recording history: %+v", loaded)
	}
	path, _ := recordingSetupPath()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("recording history must be private", err)
	}
	for _, data := range []string{`{`, `{"fps":"120","quality":"unknown","selection":{"Size":{"X":100,"Y":100}}}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		loaded := loadRecordingSetup()
		if loaded.Selection != nil || loaded.FPS != "60" || loaded.Quality != "1080p" {
			t.Fatalf("invalid history was not repaired: %+v", loaded)
		}
	}
}

func TestRecordingReuseRequiresMatchingMonitorAndScalesPixels(t *testing.T) {
	selection := testRecordingSelection()
	screen := &mygo.Display{Bounds: selection.Display}
	region, ok := reuseRecordingRegion(selection, screen, image.Pt(3840, 2160))
	if !ok || region != image.Rect(200, 200, 1800, 1400) {
		t.Fatal("high-DPI stream lost the existing crop", region, ok)
	}
	for _, tc := range []struct {
		screen *mygo.Display
		size   image.Point
	}{
		{nil, image.Pt(1920, 1080)}, // a window or unidentified source
		{&mygo.Display{Bounds: mygo.Rectangle{Width: 1920, Height: 1080}}, image.Pt(1920, 1080)},
		{screen, image.Pt(1280, 1024)}, // changed stream aspect ratio
		{screen, image.Point{}},
	} {
		if _, ok := reuseRecordingRegion(selection, tc.screen, tc.size); ok {
			t.Fatalf("reused region on a different source: %+v", tc)
		}
	}
}

func TestRecordingRepeatControlsStayVisibleAndRestoreQuality(t *testing.T) {
	for _, size := range [][2]int{{400, 340}, {480, 500}} {
		for _, dark := range []bool{false, true} {
			s := &application{recordingSetup: recordingSetup{Selection: testRecordingSelection(), FPS: "30", Quality: "1440p"}}
			v := newVideoWindow(s, nil, nil)
			if v.fps != "30" || v.quality != "1440p" {
				t.Fatal("reopened panel reset encoder settings")
			}
			tt := ui.NewTester(v.view, size[0], size[1])
			tt.SetDark(dark)
			for _, label := range []string{"重新选区…", "录制上次区域"} {
				r, ok := tt.Find(label)
				if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > float32(size[0])+.5 || r.Y+r.H > float32(size[1])+.5 {
					t.Fatalf("%s hidden at %v: %+v", label, size, r)
				}
			}
			saveShortcutPreview(t, tt, "recording-repeat-"+v.fps+"-dark-"+map[bool]string{true: "true", false: "false"}[dark])
		}
	}
}
