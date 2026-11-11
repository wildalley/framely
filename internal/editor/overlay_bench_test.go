package editor

import (
	"image"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// benchPicker is a 4K picker drawn by the software renderer, as on a
// machine without a GPU.
func benchPicker(b *testing.B, opts PickOptions) (*pick, *ui.Tester) {
	full := image.NewRGBA(image.Rect(0, 0, 3840, 2160))
	for i := range full.Pix {
		full.Pix[i] = byte(i)
	}
	v := &pickView{display: mygo.Display{Bounds: mygo.Rectangle{Width: 3840, Height: 2160}}, crop: full.Bounds()}
	s := &pick{full: full, bitmap: ui.NewBitmap(full).Pixelated(), result: make(chan pickResult, 1), opts: opts}
	tt := ui.NewTester(func(c *ui.Context) { s.view(c, v) }, 3840, 2160)
	tt.Move(100, 100)
	return s, tt
}

// One pointer move over the picker: what each mouse event costs while the
// magnifier follows the pointer.
func BenchmarkPickerPointerMove4K(b *testing.B) {
	_, tt := benchPicker(b, PickOptions{Minimum: image.Pt(4, 4), Actions: true})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tt.Move(float32(200+i%800), float32(200+i%600))
	}
}
