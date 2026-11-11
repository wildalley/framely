package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/wildalley/framely/internal/doc"
)

func gray(w, h int, v uint8) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = v
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	return img
}

func TestFlattenDrawsAnnotations(t *testing.T) {
	src := gray(200, 100, 200)
	rect := doc.New(doc.ToolRectangle, doc.Point{X: 20, Y: 20}, doc.Style{Color: "#2383e2", StrokeWidth: 6, Opacity: 1, FontSize: 28})
	rect.Width, rect.Height = 100, 50
	out, err := Flatten(src, nil, []doc.Annotation{rect})
	if err != nil {
		t.Fatal(err)
	}
	if c := out.RGBAAt(20, 45); c.B < 180 || c.R > 80 {
		t.Fatalf("no stroke on the left edge: %v", c)
	}
	if c := out.RGBAAt(70, 45); c != (color.RGBA{200, 200, 200, 255}) {
		t.Fatalf("a transparent fill painted the inside: %v", c)
	}
	if src.RGBAAt(20, 45) != (color.RGBA{200, 200, 200, 255}) {
		t.Fatal("flatten drew into the source image")
	}
}

func TestMosaicNeedsItsPixels(t *testing.T) {
	src := gray(64, 64, 100)
	m := doc.New(doc.ToolMosaic, doc.Point{}, doc.DefaultStyle(doc.ToolMosaic))
	m.Width, m.Height = 30, 30
	if _, err := Flatten(src, nil, []doc.Annotation{m}); err != ErrMosaicUnavailable {
		t.Fatalf("exported a mosaic without its pixelation: %v", err)
	}
	if _, err := Flatten(src, Pixelate(src), []doc.Annotation{m}); err != nil {
		t.Fatal(err)
	}
}

func TestAreaHighlightDimsOutside(t *testing.T) {
	src := gray(100, 100, 200)
	a := doc.New(doc.ToolAreaHighlight, doc.Point{X: 10, Y: 10}, doc.DefaultStyle(doc.ToolAreaHighlight))
	a.Width, a.Height = 40, 40
	b := a
	b.ID, b.X, b.Y = "b", 30, 30 // overlaps a
	out, err := Flatten(src, nil, []doc.Annotation{a, b})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []image.Point{{20, 20}, {40, 40}, {60, 60}} {
		if c := out.RGBAAt(p.X, p.Y); c.R != 200 {
			t.Errorf("highlighted %v dimmed: %v", p, c)
		}
	}
	if c := out.RGBAAt(90, 5); c.R >= 150 {
		t.Errorf("outside not dimmed: %v", c)
	}
}

func TestPixelateAverages(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := range 8 {
		for x := range 16 {
			v := uint8(0)
			if x%2 == 0 {
				v = 200
			}
			img.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
	small := Pixelate(img)
	if small.Bounds().Dx() != 2 || small.Bounds().Dy() != 1 {
		t.Fatalf("size %v", small.Bounds())
	}
	if c := small.RGBAAt(0, 0); c.R != 100 {
		t.Fatalf("average %v", c)
	}
}

func TestEncodeJPEGOnWhite(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8)) // transparent
	data, err := Encode(img, JPEG, 0.9)
	if err != nil || len(data) == 0 {
		t.Fatal(err)
	}
}
