package editor

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/capture"
	"github.com/wildalley/framely/internal/doc"
)

// actionPicker is an actions picker on one 800×600 display whose pixels
// are twice its DIPs.
func actionPicker() (*pick, *pickView, *ui.Tester) {
	full := image.NewRGBA(image.Rect(0, 0, 1600, 1200))
	for y := 0; y < 1200; y++ {
		for x := 0; x < 1600; x++ {
			full.SetRGBA(x, y, color.RGBA{uint8(x / 7), uint8(y / 5), 140, 255})
		}
	}
	v := &pickView{display: mygo.Display{Bounds: mygo.Rectangle{Width: 800, Height: 600}}, crop: full.Bounds()}
	s := &pick{full: full, bitmap: ui.NewBitmap(full).Pixelated(), result: make(chan pickResult, 1),
		opts: PickOptions{Minimum: image.Pt(4, 4), Actions: true, Scroll: true, Record: true, Text: true}}
	tt := ui.NewTester(func(c *ui.Context) { s.view(c, v) }, 800, 600)
	return s, v, tt
}

// waitPick fails rather than hangs when the picker never confirms.
func waitPick(t *testing.T, s *pick) pickResult {
	t.Helper()
	select {
	case r := <-s.result:
		return r
	default:
		t.Fatalf("the picker did not finish: done=%v adjusting=%v sel=%+v", s.done, s.adjusting, s.sel)
		return pickResult{}
	}
}

func dragPick(tt *ui.Tester, x0, y0, x1, y1 float32) {
	tt.Press(x0, y0)
	tt.Move((x0+x1)/2, (y0+y1)/2)
	tt.Move(x1, y1)
	tt.Release(x1, y1)
}

func TestReleasedRegionStaysAdjustableUntilAnActionIsChosen(t *testing.T) {
	s, _, tt := actionPicker()
	dragPick(tt, 100, 100, 300, 250)
	if s.done || !s.adjusting || s.sel != (doc.Rect{X: 100, Y: 100, W: 200, H: 150}) {
		t.Fatalf("release confirmed or lost the region: done=%v adjusting=%v sel=%+v", s.done, s.adjusting, s.sel)
	}
	bar, ok := tt.Find("选区操作")
	if !ok || bar.Y < 250 || bar.X < 0 || bar.X+bar.W > 800 {
		t.Fatalf("the action bar is not under the region: %+v", bar)
	}
	// Move it by dragging inside, then stretch it by its bottom-right handle.
	dragPick(tt, 200, 175, 230, 195)
	dragPick(tt, 330, 270, 400, 300)
	if s.sel != (doc.Rect{X: 130, Y: 120, W: 270, H: 180}) {
		t.Fatalf("move and resize: %+v", s.sel)
	}
	tt.Key(ui.Shift, ui.KeyLeft)
	tt.Key(0, ui.KeyUp)
	if s.sel.X != 120 || s.sel.Y != 119 {
		t.Fatalf("arrow nudge: %+v", s.sel)
	}
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	choice := waitPick(t, s)
	if choice.err != nil || choice.Action != PickCopy || choice.Region != image.Rect(240, 238, 780, 598) {
		t.Fatalf("copy choice: %+v", choice)
	}
}

func TestPickerActionsReportWhatToDo(t *testing.T) {
	for _, tc := range []struct {
		label  string
		action PickAction
		tool   doc.Tool
	}{
		{"长截图", PickScroll, ""},
		{"录屏", PickRecord, ""},
		{"取字", PickText, ""},
		{"钉在屏幕上（双击选区）", PickPin, ""},
		{"打开编辑窗口", PickEdit, ""},
		{"箭头", PickEdit, doc.ToolArrow},
	} {
		s, _, tt := actionPicker()
		dragPick(tt, 100, 100, 300, 250)
		if err := tt.Click(tc.label); err != nil {
			t.Fatal(tc.label, err)
		}
		choice := waitPick(t, s)
		if choice.Action != tc.action || choice.Tool != tc.tool || choice.Region.Empty() {
			t.Fatalf("%s: %+v", tc.label, choice)
		}
	}
}

func TestPickerKeysAndClicks(t *testing.T) {
	// Enter on an empty screen takes all of it, and Enter again copies.
	s, _, tt := actionPicker()
	tt.Key(0, ui.KeyEnter)
	if !s.adjusting || s.sel != (doc.Rect{W: 800, H: 600}) {
		t.Fatalf("Enter did not take the whole display: %+v", s.sel)
	}
	tt.Key(0, ui.KeyEnter)
	if choice := waitPick(t, s); choice.Action != PickCopy || choice.Region != image.Rect(0, 0, 1600, 1200) {
		t.Fatalf("Enter on a region: %+v", choice)
	}

	// A double click inside pins; R brings the last region back.
	s, _, tt = actionPicker()
	dragPick(tt, 100, 100, 300, 250)
	tt.Press(150, 150)
	tt.Release(150, 150)
	tt.Press(150, 150)
	tt.Release(150, 150)
	if choice := waitPick(t, s); choice.Action != PickPin {
		t.Fatalf("double click: %+v", choice)
	}
	s, _, tt = actionPicker()
	tt.Key(0, ui.KeyR)
	if !s.adjusting || s.sel != (doc.Rect{X: 100, Y: 100, W: 200, H: 150}) {
		t.Fatalf("R did not restore the last region: %+v", s.sel)
	}

	// A right click clears the region, the next one cancels.
	tt.RightClickAt(400, 400)
	if s.adjusting || s.done {
		t.Fatal("a right click on a region did not clear it")
	}
	tt.RightClickAt(400, 400)
	if choice := waitPick(t, s); choice.err != capture.ErrCancelled {
		t.Fatalf("second right click: %+v", choice)
	}
}

func TestMagnifierReadsTheSourcePixelUnderThePointer(t *testing.T) {
	s, v, tt := actionPicker()
	tt.Move(100, 50)
	hex, ok := s.colorAt(v, s.pointer, 800, 600)
	if !ok || hex != fmt.Sprintf("#%02X%02X8C", 200/7, 100/5) {
		t.Fatalf("color under the pointer: %s %v", hex, ok)
	}
}

// Renders the picker for visual review; set SCREENPIN_UI_SCREENSHOTS.
func TestPickerPreview(t *testing.T) {
	out := os.Getenv("SCREENPIN_UI_SCREENSHOTS")
	if out == "" {
		t.Skip("set SCREENPIN_UI_SCREENSHOTS to a directory")
	}
	for _, dark := range []bool{false, true} {
		s, _, tt := actionPicker()
		if demo := os.Getenv("SCREENPIN_PREVIEW_IMAGE"); demo != "" {
			data, err := os.ReadFile(demo)
			if err != nil {
				t.Fatal(err)
			}
			full, err := capture.Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			v := &pickView{display: mygo.Display{Bounds: mygo.Rectangle{Width: 800, Height: 600}}, crop: full.Bounds()}
			s = &pick{full: full, bitmap: ui.NewBitmap(full).Pixelated(), result: make(chan pickResult, 1), opts: s.opts}
			tt = ui.NewTester(func(c *ui.Context) { s.view(c, v) }, 800, 600)
		}
		tt.SetDark(dark)
		tt.Move(230, 140)
		save := func(name string) {
			f, err := os.Create(filepath.Join(out, fmt.Sprintf("%s-dark-%t.png", name, dark)))
			if err != nil {
				t.Fatal(err)
			}
			_ = png.Encode(f, tt.Image())
			f.Close()
		}
		save("picker-magnifier")
		dragPick(tt, 380, 140, 720, 400)
		tt.Move(600, 300)
		save("picker-actions")
	}
}

func TestColorPickerPicksNoRegionAndCancelsAfterCopying(t *testing.T) {
	var copied string
	saved := writeColor
	writeColor = func(hex string) { copied = hex }
	defer func() { writeColor = saved }()
	s, _, tt := actionPicker()
	s.opts = PickOptions{Color: true}
	tt.Press(100, 100)
	tt.Move(300, 300)
	tt.Release(300, 300)
	if s.adjusting || s.dragging {
		t.Fatal("the color picker drew a region")
	}
	if r, ok := tt.Find("选区操作"); ok && r.W > 0 {
		t.Fatal("the color picker shows the region actions")
	}
	// The click copied the color and closed; nothing opens.
	if choice := waitPick(t, s); choice.err != capture.ErrCancelled || copied != fmt.Sprintf("#%02X%02X8C", 200/7, 200/5) {
		t.Fatalf("color pick: %+v copied %q", choice, copied)
	}
}

func TestRepeatPickMapsTheLastRegionOntoANewDesktopImage(t *testing.T) {
	lastPick.ok = false
	display := mygo.Display{Bounds: mygo.Rectangle{Width: 800, Height: 600}}
	full := image.NewRGBA(image.Rect(0, 0, 1600, 1200))
	if _, err := RepeatPick(full, []mygo.Display{display}); err == nil || HasLastPick() {
		t.Fatal("repeated a region before any was picked")
	}
	s, _, tt := actionPicker()
	dragPick(tt, 100, 100, 300, 250)
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	waitPick(t, s)
	choice, err := RepeatPick(full, []mygo.Display{display})
	if err != nil || choice.Region != image.Rect(200, 200, 600, 500) || choice.Target == nil || *choice.Target != display.Bounds {
		t.Fatalf("repeat: %+v %v", choice, err)
	}
	other := mygo.Display{Bounds: mygo.Rectangle{X: 800, Width: 800, Height: 600}}
	if _, err := RepeatPick(full, []mygo.Display{other}); err == nil {
		t.Fatal("repeated a region on a display that is gone")
	}
}
