package render

import (
	"errors"
	"image"
	"image/color"
	"testing"

	"github.com/wildalley/framely/internal/doc"
)

func TestOCRRegionMatchesFullExportIncludingPrivacyMasks(t *testing.T) {
	for _, origin := range []image.Point{{}, {17, 23}} {
		src := image.NewRGBA(image.Rect(0, 0, 120, 80).Add(origin))
		for y := src.Rect.Min.Y; y < src.Rect.Max.Y; y++ {
			for x := src.Rect.Min.X; x < src.Rect.Max.X; x++ {
				src.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), A: 255})
			}
		}
		mask := doc.New(doc.ToolMosaic, doc.Point{X: 10, Y: 10}, doc.DefaultStyle(doc.ToolMosaic))
		mask.Width, mask.Height = 80, 40
		box := doc.New(doc.ToolRectangle, doc.Point{X: 5, Y: 5}, doc.DefaultStyle(doc.ToolRectangle))
		box.Width, box.Height, box.Fill = 60, 30, "#e5484d"
		for _, list := range [][]doc.Annotation{nil, {box, mask}} {
			for _, ratio := range []float64{0, 1} {
				full, err := FlattenWithCorners(src, gray(120, 80, 180), list, ratio)
				if err != nil {
					t.Fatal(err)
				}
				for _, region := range []image.Rectangle{image.Rect(0, 0, 70, 45), image.Rect(30, 15, 110, 75)} {
					out, err := FlattenRegion(src, gray(120, 80, 180), list, ratio, region.Add(origin))
					if err != nil {
						t.Fatal(err)
					}
					if out.Rect != image.Rect(0, 0, region.Dx(), region.Dy()) {
						t.Fatal(out.Rect)
					}
					for y := 0; y < out.Rect.Dy(); y++ {
						for x := 0; x < out.Rect.Dx(); x++ {
							want := full.RGBAAt(full.Rect.Min.X+region.Min.X+x, full.Rect.Min.Y+region.Min.Y+y)
							if got := out.RGBAAt(x, y); got != want {
								t.Fatalf("origin=%v ratio=%g region=%v pixel=%d,%d: %v != %v", origin, ratio, region, x, y, got, want)
							}
						}
					}
				}
			}
		}
		if _, err := FlattenRegion(src, nil, []doc.Annotation{mask}, 0, src.Rect); !errors.Is(err, ErrMosaicUnavailable) {
			t.Fatal("missing mask exposed source", err)
		}
	}
}

func TestOCRRegionOwnsOnlyCroppedPixels(t *testing.T) {
	src := gray(120, 80, 90)
	out, err := FlattenRegion(src, nil, nil, 0, image.Rect(20, 30, 40, 50))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Pix) != 20*20*4 {
		t.Fatal("crop retained full image backing storage")
	}
	out.SetRGBA(0, 0, color.RGBA{})
	if src.RGBAAt(20, 30).A != 255 {
		t.Fatal("crop modified source")
	}
}
