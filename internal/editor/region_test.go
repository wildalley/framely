package editor

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRegionCoordinatesSurviveLetterboxingAndDisplayScale(t *testing.T) {
	for _, scale := range []float32{1, 1.5, 2} {
		p := &imagePicker{bounds: image.Rect(0, 0, 1000, 500), bitmap: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 1000, 500))), result: make(chan regionChoice, 1)}
		tt := ui.NewTester(p.view, 600, 500)
		tt.SetScale(scale)
		box, ok := tt.Find("图片选区")
		if !ok {
			t.Fatal("missing region canvas")
		}
		r := p.imageRect
		tt.Press(box.X+r.X+r.W/4, box.Y+r.Y+r.H/4)
		tt.Move(box.X+r.X+r.W*3/4, box.Y+r.Y+r.H*3/4)
		tt.Release(box.X+r.X+r.W*3/4, box.Y+r.Y+r.H*3/4)
		tt.Key(0, ui.KeyEnter)
		select {
		case choice := <-p.result:
			if choice.err != nil || choice.rect.Min.X < 249 || choice.rect.Min.X > 251 || choice.rect.Min.Y < 124 || choice.rect.Min.Y > 126 || choice.rect.Dx() < 498 || choice.rect.Dx() > 502 {
				t.Fatalf("scale=%g choice=%+v", scale, choice)
			}
		default:
			t.Fatalf("scale=%g did not confirm", scale)
		}
	}
}

func TestRegionCanResizeAndMoveWithoutRedrawing(t *testing.T) {
	for _, scale := range []float32{1, 1.5, 2} {
		p := &imagePicker{bounds: image.Rect(0, 0, 1000, 500), bitmap: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 1000, 500))), result: make(chan regionChoice, 1)}
		tt := ui.NewTester(p.view, 600, 500)
		tt.SetScale(scale)
		box, _ := tt.Find("图片选区")
		point := func(x, y float32) (float32, float32) {
			return box.X + p.imageRect.X + x*p.imageRect.W/1000, box.Y + p.imageRect.Y + y*p.imageRect.H/500
		}
		drag := func(x0, y0, x1, y1 float32) {
			x, y := point(x0, y0)
			tt.Press(x, y)
			x, y = point(x1, y1)
			tt.Move(x, y)
			tt.Release(x, y)
		}
		drag(200, 100, 600, 300)
		drag(600, 200, 700, 200)
		r := p.selection()
		if r.Dx() < 499 || r.Dx() > 501 || r.Dy() < 199 || r.Dy() > 201 {
			t.Fatalf("scale %g resized by drawing a new selection: %v", scale, r)
		}
		drag(450, 200, 500, 225)
		r = p.selection()
		if r.Min.X < 249 || r.Min.X > 251 || r.Min.Y < 124 || r.Min.Y > 126 || r.Dx() < 499 {
			t.Fatalf("scale %g move: %v", scale, r)
		}
		tt.Key(0, ui.KeyRight)
		tt.Key(ui.Shift, ui.KeyRight)
		after := p.selection()
		if after.Min.X != r.Min.X+1 || after.Max.X != r.Max.X+2 || after.Dy() != r.Dy() {
			t.Fatalf("keyboard adjustment: %v -> %v", r, after)
		}
		tt.Key(0, ui.KeyEnter)
		if choice := <-p.result; choice.rect != after || choice.err != nil {
			t.Fatal(choice)
		}
	}
}
