package render

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"testing"

	"github.com/wildalley/screenpin/internal/doc"
	"golang.org/x/image/webp"
)

func decodeWebP(t *testing.T, data []byte) image.Image {
	t.Helper()
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		t.Fatal("export does not contain a WebP RIFF header")
	}
	// Decode independently of the encoder, as imported images are decoded.
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestWebPExportPreservesAlphaAndCroppedRows(t *testing.T) {
	parent := image.NewRGBA(image.Rect(0, 0, 128, 96))
	img := parent.SubImage(image.Rect(9, 7, 105, 71)).(*image.RGBA)
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			alpha := []uint8{0, 128, 255}[(x-img.Rect.Min.X)/32]
			img.Set(x, y, color.NRGBA{R: 220, G: 110, B: 40, A: alpha})
		}
	}
	before := bytes.Clone(parent.Pix)
	data, err := Encode(img, WebP, 1)
	if err != nil {
		t.Fatal(err)
	}
	out := decodeWebP(t, data)
	if out.Bounds() != image.Rect(0, 0, 96, 64) {
		t.Fatalf("exported bounds = %v", out.Bounds())
	}
	for i, alpha := range []uint8{0, 128, 255} {
		c := color.NRGBAModel.Convert(out.At(i*32+16, 32)).(color.NRGBA)
		if c.A != alpha {
			t.Errorf("band %d alpha = %d, want %d", i, c.A, alpha)
		}
		// Lossy YUV chroma conversion can shift RGB even at quality 100.
		// Premultiplied input would instead roughly halve this band's RGB.
		if alpha > 0 && (abs(int(c.R)-220) > 20 || abs(int(c.G)-110) > 20 || abs(int(c.B)-40) > 20) {
			t.Errorf("band %d straight color darkened: %v", i, c)
		}
	}
	semi := color.NRGBAModel.Convert(out.At(48, 32)).(color.NRGBA)
	opaque := color.NRGBAModel.Convert(out.At(80, 32)).(color.NRGBA)
	if abs(int(semi.R)-int(opaque.R)) > 3 || abs(int(semi.G)-int(opaque.G)) > 3 || abs(int(semi.B)-int(opaque.B)) > 3 {
		t.Fatalf("alpha changed visible color: semi=%v opaque=%v", semi, opaque)
	}
	if !bytes.Equal(before, parent.Pix) {
		t.Fatal("encoding changed the original image")
	}
}

func TestWebPQualityChangesSizeAndFidelity(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 96, 64))
	seed := uint32(42)
	for y := range 64 {
		for x := range 96 {
			seed = seed*1664525 + 1013904223
			img.SetRGBA(x, y, color.RGBA{uint8(seed >> 24), uint8(seed >> 16), uint8(seed >> 8), 255})
		}
	}
	var sizes [2]int
	var distance [2]int64
	for i, q := range []float64{0.6, 1} {
		data, err := Encode(img, WebP, q)
		if err != nil {
			t.Fatal(err)
		}
		sizes[i] = len(data)
		out := decodeWebP(t, data)
		for y := range 64 {
			for x := range 96 {
				a := img.RGBAAt(x, y)
				b := color.RGBAModel.Convert(out.At(x, y)).(color.RGBA)
				for _, d := range []int{int(a.R) - int(b.R), int(a.G) - int(b.G), int(a.B) - int(b.B)} {
					distance[i] += int64(d * d)
				}
			}
		}
	}
	if sizes[1] <= sizes[0] || distance[1] >= distance[0] {
		t.Fatalf("quality had no useful effect: sizes=%v squared error=%v", sizes, distance)
	}
}

func TestWebPExportContainsMosaic(t *testing.T) {
	img := gray(64, 32, 0)
	for y := range 32 {
		for x := range 64 {
			if x%2 == 0 {
				img.SetRGBA(x, y, color.RGBA{200, 200, 200, 255})
			}
		}
	}
	m := doc.New(doc.ToolMosaic, doc.Point{}, doc.DefaultStyle(doc.ToolMosaic))
	m.Width, m.Height = 32, 32
	if _, err := Flatten(img, nil, []doc.Annotation{m}); !errors.Is(err, ErrMosaicUnavailable) {
		t.Fatalf("missing mosaic was not blocked: %v", err)
	}
	flat, err := Flatten(img, Pixelate(img), []doc.Annotation{m})
	if err != nil {
		t.Fatal(err)
	}
	data, err := Encode(flat, WebP, 1)
	if err != nil {
		t.Fatal(err)
	}
	out := decodeWebP(t, data)
	for _, x := range []int{14, 15, 16, 17} {
		c := color.RGBAModel.Convert(out.At(x, 16)).(color.RGBA)
		if abs(int(c.R)-100) > 8 || abs(int(c.G)-100) > 8 || abs(int(c.B)-100) > 8 {
			t.Fatalf("mosaic leaked source stripes at %d: %v", x, c)
		}
	}
}

func TestEncodeRejectsCancelledOrUnsupportedOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	img := gray(8, 8, 100)
	for _, format := range Formats {
		if data, err := EncodeContext(ctx, img, format, 0.92); !errors.Is(err, context.Canceled) || data != nil {
			t.Fatalf("%s returned cancelled output: %d bytes, %v", format, len(data), err)
		}
	}
	if data, err := Encode(img, Format("unsupported"), 1); err == nil || data != nil {
		t.Fatal("unknown format silently produced a different file format")
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
