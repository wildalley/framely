package render

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/wildalley/framely/internal/doc"
)

func TestRoundedScreenshotClipsPixelsAndAnnotationsAndPreservesExportAlpha(t *testing.T) {
	src := gray(120, 80, 100)
	annotation := doc.New(doc.ToolRectangle, doc.Point{}, doc.DefaultStyle(doc.ToolRectangle))
	annotation.Width, annotation.Height = 120, 80
	annotation.Fill = "#e5484d"
	for _, list := range [][]doc.Annotation{nil, {annotation}} {
		out, err := FlattenWithCorners(src, nil, list, 1)
		if err != nil {
			t.Fatal(err)
		}
		if out.RGBAAt(0, 0).A != 0 || out.RGBAAt(60, 40).A != 255 || out.RGBAAt(60, 1).A != 255 {
			t.Fatalf("rounded alpha is incorrect: corner=%v center=%v top=%v", out.RGBAAt(0, 0), out.RGBAAt(60, 40), out.RGBAAt(60, 1))
		}
		for _, format := range []Format{PNG, WebP, JPEG} {
			data, err := Encode(out, format, 1)
			if err != nil {
				t.Fatal(err)
			}
			decoded, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			corner := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA)
			if format == JPEG {
				if corner.R < 245 || corner.G < 245 || corner.B < 245 {
					t.Fatal("JPEG corners were not composited on white", corner)
				}
			} else if corner.A != 0 {
				t.Fatalf("%s lost rounded transparency: %v", format, corner)
			}
		}
	}
	if src.RGBAAt(0, 0) != (color.RGBA{100, 100, 100, 255}) {
		t.Fatal("rounding modified source pixels")
	}
}

func TestRectangleRoundnessAffectsItsVisibleCorners(t *testing.T) {
	a := doc.New(doc.ToolRectangle, doc.Point{X: 20, Y: 20}, doc.DefaultStyle(doc.ToolRectangle))
	a.Width, a.Height = 80, 40
	a.Fill = "#e5484d"
	style := a.Style()
	style.CornerRatio = 0
	a.SetStyle(style)
	square, err := Flatten(gray(120, 80, 100), nil, []doc.Annotation{a})
	if err != nil {
		t.Fatal(err)
	}
	style.CornerRatio = 1
	a.SetStyle(style)
	round, err := Flatten(gray(120, 80, 100), nil, []doc.Annotation{a})
	if err != nil {
		t.Fatal(err)
	}
	if square.RGBAAt(22, 22).R < 180 || round.RGBAAt(22, 22).R != 100 || round.RGBAAt(60, 25).R < 180 {
		t.Fatal("rectangle roundness did not change the rendered curve")
	}
}

func TestRoundedLongImageKeepsItsFullResolution(t *testing.T) {
	out, err := FlattenWithCorners(gray(64, 9000, 100), nil, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Rect != image.Rect(0, 0, 64, 9000) || out.RGBAAt(32, 8999).A < 245 || out.RGBAAt(0, 8999).A != 0 {
		t.Fatalf("long image rounding: bounds=%v bottom=%v corner=%v", out.Rect, out.RGBAAt(32, 8999), out.RGBAAt(0, 8999))
	}
}
