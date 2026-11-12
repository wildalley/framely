package scrollshot

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math/rand/v2"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func tableDocument(w, h int) *image.RGBA {
	page := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(page, page.Bounds(), image.White, image.Point{}, draw.Src)
	pen := font.Drawer{Dst: page, Src: image.NewUniform(color.RGBA{65, 65, 65, 255}), Face: basicfont.Face7x13}
	rng := rand.New(rand.NewPCG(51, 117))
	for y := 0; y < h-64; y += 64 {
		for _, cell := range []struct {
			x    int
			text string
		}{
			{20, fmt.Sprintf("Member %08x", rng.Uint32())},
			{440, "3 days overdue"}, {660, "364 days inactive"}, {990, "2026-11-09"}, {1280, "2025-11-10"}, {1710, "Coach"}, {2000, "Follow / Edit"},
		} {
			pen.Dot = fixed.P(cell.x, y+30)
			pen.DrawString(cell.text)
		}
		pen.Dot = fixed.P(20, y+47)
		pen.DrawString(fmt.Sprintf("13%09d", rng.IntN(1_000_000_000)))
		draw.Draw(page, image.Rect(0, y+63, w, y+64), image.NewUniform(color.RGBA{235, 235, 235, 255}), image.Point{}, draw.Src)
	}
	return page
}

func tableViewport(page *image.RGBA, at, body, top, bottom int) *image.RGBA {
	frame := viewport(page, at, body, top, bottom)
	draw.Draw(frame, image.Rect(0, 0, page.Rect.Dx(), top), image.White, image.Point{}, draw.Src)
	draw.Draw(frame, image.Rect(0, top+body, page.Rect.Dx(), frame.Rect.Dy()), image.White, image.Point{}, draw.Src)
	pen := font.Drawer{Dst: frame, Src: image.Black, Face: basicfont.Face7x13, Dot: fixed.P(20, top-15)}
	pen.DrawString("Member             Follow-up             Due date       Last activity      Coach       Actions")
	pen.Dot = fixed.P(page.Rect.Dx()-300, frame.Rect.Dy()-12)
	pen.DrawString("Page 1 / 1    Previous / Next")
	return frame
}

func TestWideTableWithRepeatedDatesAndStickyFooter(t *testing.T) {
	page := tableDocument(2250, 3000)
	s := New()
	for _, at := range []int{0, 2, 17, 83, 95, 159, 173, 241, 257, 389, 407, 512, 641} {
		frame := tableViewport(page, at, 590, 150, 55)
		out, err := s.Add(frame)
		if err != nil || out.Height != 795+at {
			t.Fatalf("table at %d: %+v %v", at, out, err)
		}
	}
	got, want := s.Flatten(), tableViewport(page, 0, 590+641, 150, 55)
	for y := range got.Rect.Dy() {
		for x := range got.Rect.Dx() {
			if got.RGBAAt(x, y) != want.RGBAAt(x, y) {
				t.Fatalf("lost/repeated table pixel %d,%d", x, y)
			}
		}
	}
}

func TestScrollbarThumbFragmentsAreRemovedWithoutChangingWidth(t *testing.T) {
	page := document(200, 1000)
	s := New()
	for _, at := range []int{0, 45, 95, 127} {
		frame := viewport(page, at, 240, 20, 16)
		draw.Draw(frame, image.Rect(188, 0, 196, 276), image.White, image.Point{}, draw.Src)
		draw.Draw(frame, image.Rect(190, 25+at/2, 196, 105+at/2), image.NewUniform(color.RGBA{145, 145, 145, 255}), image.Point{}, draw.Src)
		if _, err := s.Add(frame); err != nil {
			t.Fatal(err)
		}
	}
	if s.scrollbar.Empty() {
		t.Fatal("scrollbar was not recognized")
	}
	got := s.Flatten()
	if got.Rect.Dx() != 200 || got.Rect.Dy() != 403 {
		t.Fatal("scrollbar cleanup cropped the image")
	}
	view := s.Image()
	for y := range got.Rect.Dy() {
		for x := s.scrollbar.Min.X; x < s.scrollbar.Max.X; x++ {
			if got.RGBAAt(x, y) != got.RGBAAt(s.scrollbar.Min.X-2, y) || view.At(x, y) != got.At(x, y) {
				t.Fatal("scrollbar fragments remain or preview differs")
			}
		}
	}
}

func TestScrollbarRecognitionPreservesTextAndSolidBorder(t *testing.T) {
	page := tableDocument(2250, 1000)
	if !scrollbarStrip(page).Empty() {
		t.Fatal("table text mistaken for scrollbar")
	}
	draw.Draw(page, image.Rect(2240, 0, 2246, 1000), image.NewUniform(color.RGBA{145, 145, 145, 255}), image.Point{}, draw.Src)
	if !scrollbarStrip(page).Empty() {
		t.Fatal("full-height border mistaken for scrollbar")
	}
}

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
	if s.Height() != 0 || s.previous != nil || s.sidebarFrame != nil || len(s.chunks) != 0 {
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

// Fixed navigation must remain anchored once while only the article grows.
func TestFixedSidebarsKeepNavigationOnce(t *testing.T) {
	for _, sides := range []struct {
		left, right int
		bg          color.RGBA
	}{
		{120, 0, color.RGBA{235, 235, 235, 255}}, {0, 100, color.RGBA{235, 235, 235, 255}}, {120, 100, color.RGBA{235, 235, 235, 255}},
		{120, 0, color.RGBA{30, 35, 35, 255}}, {0, 100, color.RGBA{30, 35, 35, 255}}, {120, 100, color.RGBA{30, 35, 35, 255}},
	} {
		t.Run(fmt.Sprintf("left_%d_right_%d_bg_%d", sides.left, sides.right, sides.bg.R), func(t *testing.T) {
			const w, h, top, bottom = 480, 320, 20, 16
			page := document(w, 1500)
			frame := func(at int) *image.RGBA {
				f := viewport(page, at, h-top-bottom, top, bottom)
				for _, r := range []image.Rectangle{image.Rect(0, 0, sides.left, h), image.Rect(w-sides.right, 0, w, h)} {
					draw.Draw(f, r, image.NewUniform(sides.bg), image.Point{}, draw.Src)
					if !r.Empty() {
						draw.Draw(f, image.Rect(r.Min.X+10, top+25, r.Max.X-10, top+45), image.Black, image.Point{}, draw.Src)
						draw.Draw(f, image.Rect(r.Min.X+10, top+100, r.Max.X-10, top+120), image.Black, image.Point{}, draw.Src)
						// A menu item/avatar crosses the last viewport row. Copying
						// that row to extend the sidebar creates vertical streaks.
						for x := r.Min.X + 10; x < r.Max.X-10; x += 3 {
							draw.Draw(f, image.Rect(x, h-30, x+1, h), image.Black, image.Point{}, draw.Src)
						}
					}
				}
				return f
			}
			first := frame(0)
			s := New()
			for _, at := range []int{0, 45, 95, 150, 210} {
				out, err := s.Add(frame(at))
				if err != nil || out.Height != h+at {
					t.Fatalf("at %d: %+v %v", at, out, err)
				}
			}
			check := func(at int) {
				t.Helper()
				got, view := s.Flatten(), s.Image()
				want := viewport(page, 0, h-top-bottom+at, top, bottom)
				for y := 0; y < want.Rect.Dy(); y++ {
					for x := range w {
						expected := want.RGBAAt(x, y)
						if x < sides.left || x >= w-sides.right {
							if y < h {
								expected = first.RGBAAt(x, y)
							} else {
								expected = sides.bg
							}
						}
						if got.RGBAAt(x, y) != expected {
							t.Fatalf("wrong composite pixel %d,%d at scroll %d", x, y, at)
						}
						if view.At(x, y) != got.At(x, y) {
							t.Fatalf("preview differs at %d,%d", x, y)
						}
					}
				}
			}
			check(210)
			if !s.Undo() {
				t.Fatal("undo failed")
			}
			check(150)
			if _, err := s.Add(frame(210)); err != nil {
				t.Fatal(err)
			}
			check(210)
		})
	}
}

func TestFixedSidebarUndoRestoresInitialViewportAndRelease(t *testing.T) {
	page := document(300, 1000)
	frame := func(at int) *image.RGBA {
		f := viewport(page, at, 240, 20, 16)
		draw.Draw(f, image.Rect(0, 20, 90, 260), image.NewUniform(color.RGBA{230, 230, 230, 255}), image.Point{}, draw.Src)
		draw.Draw(f, image.Rect(10, 40, 70, 60), image.Black, image.Point{}, draw.Src)
		return f
	}
	first := frame(0)
	s := New()
	if _, err := s.Add(first); err != nil {
		t.Fatal(err)
	}
	initialView := s.Image()
	if _, err := s.Add(frame(50)); err != nil {
		t.Fatal(err)
	}
	if s.left == 0 {
		t.Fatal("sidebar not detected")
	}
	acceptedView := s.Image()
	if !s.Undo() || s.left != 0 || s.right != 0 {
		t.Fatal("undo retained sidebar state")
	}
	got := s.Flatten()
	for y := range first.Rect.Dy() {
		for x := range first.Rect.Dx() {
			if got.RGBAAt(x, y) != first.RGBAAt(x, y) || initialView.At(x, y) != first.At(x, y) {
				t.Fatalf("initial viewport changed at %d,%d", x, y)
			}
		}
	}
	s.Release()
	if s.sidebarFrame != nil || s.left != 0 || s.right != 0 {
		t.Fatal("release retained sidebar")
	}
	// Previously handed-out previews remain immutable after undo/release.
	if acceptedView.At(10, 90) != first.At(10, 90) {
		t.Fatal("accepted preview changed")
	}
}
