package scrollshot

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"runtime"
	"testing"
	"weak"

	xdraw "golang.org/x/image/draw"
)

func sidebarAuditFrames() []*image.RGBA {
	page := document(960, 2400)
	var frames []*image.RGBA
	for _, at := range []int{0, 80, 160, 240} {
		f := viewport(page, at, 720, 20, 20)
		draw.Draw(f, image.Rect(0, 0, 160, 760), image.NewUniform(color.RGBA{235, 235, 235, 255}), image.Point{}, draw.Src)
		draw.Draw(f, image.Rect(20, 30, 140, 50), image.Black, image.Point{}, draw.Src)
		frames = append(frames, f)
	}
	return frames
}

func BenchmarkStitchedPreview(b *testing.B) {
	s := New()
	for _, f := range sidebarAuditFrames() {
		if _, err := s.Add(f); err != nil {
			b.Fatal(err)
		}
	}
	defer s.Release()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Preview(720, 820)
	}
}

func TestSidebarCacheDoesNotPinFullFirstViewport(t *testing.T) {
	s := New()
	var first weak.Pointer[byte]
	frames := sidebarAuditFrames()
	for i, f := range frames {
		if _, err := s.Add(f); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = weak.Make(&s.previous.Pix[0])
		}
	}
	runtime.GC()
	if first.Value() != nil {
		t.Fatal("sidebar cache retained the full first viewport after overlap chunks detached")
	}
	runtime.KeepAlive(s)
}

func TestReleasedStitcherCollectsPixelsAndSidebars(t *testing.T) {
	var kept []*Stitcher
	for i := 0; i < 20; i++ {
		s := New()
		for _, f := range sidebarAuditFrames() {
			if _, err := s.Add(f); err != nil {
				t.Fatal(err)
			}
		}
		ptr := weak.Make(&s.previous.Pix[0])
		side := weak.Make(&s.sidebarFrame.Pix[0])
		s.Release()
		kept = append(kept, s)
		runtime.GC()
		if ptr.Value() != nil || side.Value() != nil {
			t.Fatal("release retained viewport pixels")
		}
	}
	runtime.KeepAlive(kept)
}

// Hide RGBA64Image to exercise x/image/draw's original generic path as the
// reference; the allocation-free path must preserve every scaled pixel.
type genericPreview struct{ image.Image }

func TestPreviewFastPathPreservesPixels(t *testing.T) {
	s := New()
	for _, f := range sidebarAuditFrames() {
		if _, err := s.Add(f); err != nil {
			t.Fatal(err)
		}
	}
	defer s.Release()
	for _, size := range []image.Point{image.Pt(200, 200), image.Pt(720, 820), image.Pt(1200, 1200)} {
		got := s.Preview(size.X, size.Y)
		want := image.NewRGBA(got.Bounds())
		source := s.Image()
		xdraw.ApproxBiLinear.Scale(want, want.Bounds(), genericPreview{source}, source.Bounds(), draw.Src, nil)
		if !bytes.Equal(got.Pix, want.Pix) {
			t.Fatalf("fast preview changed pixels at %v", size)
		}
	}
}
