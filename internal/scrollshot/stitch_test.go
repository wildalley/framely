package scrollshot

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"math/rand/v2"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func document(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(1234)
	for y := range h {
		for x := range w {
			seed = seed*1664525 + 1013904223
			img.SetRGBA(x, y, color.RGBA{uint8(seed >> 24), uint8(seed >> 16), uint8(seed >> 8), 255})
		}
	}
	return img
}
func viewport(doc *image.RGBA, at, body, top, bottom int) *image.RGBA {
	w := doc.Rect.Dx()
	out := image.NewRGBA(image.Rect(0, 0, w, top+body+bottom))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.RGBA{25, 35, 45, 255}), image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(0, top, w, top+body), doc, image.Pt(0, at), draw.Src)
	return out
}

func TestVariableScrollKeepsFixedBarsOnlyOnceAndReproducesPixels(t *testing.T) {
	doc := document(180, 1400)
	s := New()
	top, bottom, body := 22, 18, 240
	for _, at := range []int{0, 35, 106, 173, 260, 301} {
		out, err := s.Add(viewport(doc, at, body, top, bottom))
		if err != nil {
			t.Fatalf("at %d: %v", at, err)
		}
		if out.Height != top+body+bottom+at {
			t.Fatalf("at=%d height=%d", at, out.Height)
		}
	}
	got := s.Flatten()
	want := viewport(doc, 0, body+301, top, bottom)
	if got.Bounds() != want.Bounds() {
		t.Fatal(got.Bounds(), want.Bounds())
	}
	for y := range got.Rect.Dy() {
		for x := range got.Rect.Dx() {
			if got.RGBAAt(x, y) != want.RGBAAt(x, y) {
				t.Fatalf("mismatched output pixel at %d,%d", x, y)
			}
		}
	}
	view := s.Image()
	for y := range got.Rect.Dy() {
		if view.At(50, y) != got.At(50, y) {
			t.Fatalf("tile preview differs at row %d", y)
		}
	}
}

func TestDuplicateReverseAndUnrelatedFramesDoNotChangeTheResult(t *testing.T) {
	doc := document(160, 1000)
	s := New()
	for _, at := range []int{0, 55} {
		if _, err := s.Add(viewport(doc, at, 250, 20, 20)); err != nil {
			t.Fatal(err)
		}
	}
	height := s.Height()
	if out, err := s.Add(viewport(doc, 55, 250, 20, 20)); err != nil || !out.Duplicate {
		t.Fatalf("duplicate=%+v err=%v", out, err)
	}
	if _, err := s.Add(viewport(doc, 0, 250, 20, 20)); !errors.Is(err, ErrReverse) {
		t.Fatalf("reverse=%v", err)
	}
	if _, err := s.Add(viewport(doc, 600, 250, 20, 20)); err == nil {
		t.Fatal("appended a frame without overlap")
	}
	if s.Height() != height {
		t.Fatal("failed matching changed the output")
	}
}

func TestAmbiguousRepeatedRowsAreRejected(t *testing.T) {
	doc := document(160, 500)
	for y := 20; y < 500; y++ {
		copy(doc.Pix[y*doc.Stride:y*doc.Stride+160*4], doc.Pix[(y%20)*doc.Stride:(y%20)*doc.Stride+160*4])
	}
	s := New()
	if _, err := s.Add(viewport(doc, 0, 240, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(viewport(doc, 7, 240, 0, 0)); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("repeating texture was not rejected: %v", err)
	}
}

func TestUndoAndLimitsPreserveThePreviousImage(t *testing.T) {
	doc := document(160, 1000)
	s := New()
	first := viewport(doc, 0, 240, 20, 16)
	if _, err := s.Add(first); err != nil {
		t.Fatal(err)
	}
	// The caller may reuse its own frame storage after Add.
	first.SetRGBA(60, 60, color.RGBA{1, 2, 3, 255})
	if _, err := s.Add(viewport(doc, 50, 240, 20, 16)); err != nil {
		t.Fatal(err)
	}
	if !s.CanUndo() || !s.Undo() || s.Height() != 276 || s.CanUndo() {
		t.Fatal("undo failed")
	}
	want := viewport(doc, 0, 240, 20, 16)
	if s.Flatten().RGBAAt(60, 60) != want.RGBAAt(60, 60) {
		t.Fatal("retained mutable caller pixels")
	}
	s.MaxHeight = 300
	if _, err := s.Add(viewport(doc, 50, 240, 20, 16)); !errors.Is(err, ErrLimit) {
		t.Fatalf("limit=%v", err)
	}
	if s.Height() != 276 {
		t.Fatal("limit partially appended the frame")
	}
	s.Release()
	if s.Height() != 0 || s.previous != nil || len(s.chunks) != 0 {
		t.Fatal("release retained the long image")
	}
}

func TestArticleTextWithWhiteSpaceAndFixedSidebar(t *testing.T) {
	doc := image.NewRGBA(image.Rect(0, 0, 640, 2200))
	draw.Draw(doc, doc.Bounds(), image.White, image.Point{}, draw.Src)
	rng := rand.New(rand.NewPCG(42, 19))
	words := []string{"capture", "Windows", "Linux", "native", "screen", "source", "document", "recognize", "offline", "scroll", "image", "permission", "application"}
	pen := font.Drawer{Dst: doc, Src: image.Black, Face: basicfont.Face7x13}
	for y := 30; y < 2180; y += 24 {
		line := ""
		for len(line) < 52 {
			line += words[rng.IntN(len(words))] + " "
		}
		pen.Dot = fixed.P(180, y)
		pen.DrawString(line)
	}
	s := New()
	for _, at := range []int{0, 51, 138, 221, 309, 420} {
		frame := viewport(doc, at, 600, 40, 25)
		// Navigation remains stationary alongside the article.
		draw.Draw(frame, image.Rect(0, 40, 150, 640), image.NewUniform(color.RGBA{235, 235, 235, 255}), image.Point{}, draw.Src)
		pen.Dst = frame
		pen.Dot = fixed.P(10, 80)
		pen.DrawString("Navigation")
		out, err := s.Add(frame)
		if err != nil || out.Height != 665+at {
			t.Fatalf("article at %d: %+v, %v", at, out, err)
		}
	}
}
