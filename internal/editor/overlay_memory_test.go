package editor

import (
	"image"
	"runtime"
	"testing"
	"weak"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Every way out of the picker drops its image of the whole desktop, even
// while the picker and its view are still referenced; only the region's
// rectangle is remembered for R and repeating.
func TestPickerReleasesTheDesktopImageOnEveryExit(t *testing.T) {
	saved := writeColor
	writeColor = func(string) {}
	defer func() { writeColor = saved }()
	exits := map[string]func(*pick, *ui.Tester){
		"escape": func(_ *pick, tt *ui.Tester) { tt.Key(0, ui.KeyEscape) },
		"copy": func(_ *pick, tt *ui.Tester) {
			dragPick(tt, 100, 100, 300, 250)
			_ = tt.Click("复制")
		},
		"pin": func(_ *pick, tt *ui.Tester) {
			dragPick(tt, 100, 100, 300, 250)
			_ = tt.Click("钉在屏幕上（双击选区）")
		},
		"right-click": func(_ *pick, tt *ui.Tester) {
			dragPick(tt, 100, 100, 300, 250)
			tt.RightClickAt(400, 400)
			tt.RightClickAt(400, 400)
		},
		"color": func(s *pick, tt *ui.Tester) {
			s.opts.Color = true
			tt.Move(50, 50)
			tt.ClickAt(50, 50)
		},
	}
	for name, exit := range exits {
		var kept []any
		round := func() weak.Pointer[byte] {
			full := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
			pointer := weak.Make(&full.Pix[0])
			v := &pickView{display: mygo.Display{Bounds: mygo.Rectangle{Width: 960, Height: 540}}, crop: full.Bounds()}
			s := &pick{full: full, bitmap: ui.NewBitmap(full).Pixelated(), result: make(chan pickResult, 1),
				opts: PickOptions{Minimum: image.Pt(4, 4), Actions: true, Scroll: true, Record: true, Text: true}}
			tt := ui.NewTester(func(c *ui.Context) { s.view(c, v) }, 960, 540)
			tt.Move(10, 10)
			exit(s, tt)
			tt.Frame()
			if !s.done {
				t.Fatalf("%s did not end the picker", name)
			}
			kept = append(kept, s, tt, v) // Hold everything but the pixels.
			return pointer
		}
		pointer := round()
		for i := 0; i < 3 && pointer.Value() != nil; i++ {
			runtime.GC()
		}
		if pointer.Value() != nil {
			t.Fatalf("%s kept the desktop image after the picker ended", name)
		}
		runtime.KeepAlive(kept)
	}
}
