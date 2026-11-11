package editor

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"testing"

	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/imagelimit"
)

func TestPictureBudgetFollowsWindowAndPendingExport(t *testing.T) {
	start := imagelimit.Pictures.Used()
	p, err := PictureFromPixels(image.NewRGBA(image.Rect(0, 0, 100, 100)), "test")
	if err != nil {
		t.Fatal(err)
	}
	a := New(config.Default())
	a.loadPicture(p, nil)
	p.Close()
	if imagelimit.Pictures.Used() <= start {
		t.Fatal("window is not accounted for")
	}
	pending := a.snapshot()
	a.Close()
	if imagelimit.Pictures.Used() <= start {
		t.Fatal("pending export lost its reservation")
	}
	if _, err := pending(); err == nil {
		t.Fatal("export survived cancellation")
	}
	if imagelimit.Pictures.Used() != start {
		t.Fatal("closed window/task leaked reservation")
	}
}

func TestDecodeAdmissionAndFailureRelease(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	start := imagelimit.Pictures.Used()
	full, err := imagelimit.Pictures.Acquire(imagelimit.PictureBudgetBytes - start)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePicture(data.Bytes(), "test"); !errors.Is(err, imagelimit.ErrBudget) {
		full.Release()
		t.Fatal(err)
	}
	full.Release()
	// Valid metadata with truncated pixel data must return its reservation.
	if p, err := DecodePicture(data.Bytes()[:40], "truncated"); err == nil {
		p.Close()
		t.Fatal("truncated image accepted")
	}
	if imagelimit.Pictures.Used() != start {
		t.Fatal("failed decode leaked reservation")
	}
}

func TestSmallPictureDetachesDesktopBackingBuffer(t *testing.T) {
	desktop := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	region := desktop.SubImage(image.Rect(300, 200, 400, 300)).(*image.RGBA)
	p, err := PictureFromPixels(region, "selection")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.pixels.Rect != image.Rect(0, 0, 100, 100) || cap(p.pixels.Pix) != 40000 || &p.pixels.Pix[0] == &region.Pix[0] {
		t.Fatal("selection retained full desktop")
	}
}

func TestLongCaptureReservationSurvivesCompetingWindowsAndShrinks(t *testing.T) {
	start := imagelimit.Pictures.Used()
	slot, err := ReservePicture(24_000_000)
	if err != nil {
		t.Fatal(err)
	}
	defer slot.Close()
	other, err := imagelimit.Pictures.Acquire(imagelimit.PictureBudgetBytes - imagelimit.Pictures.Used())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Release()
	p, err := slot.FromPixels(image.NewRGBA(image.Rect(0, 0, 100, 200)), "long capture")
	if err != nil {
		t.Fatalf("a reserved result lost admission: %v", err)
	}
	defer p.Close()
	slot.Close()
	other.Release()
	if got, want := imagelimit.Pictures.Used(), start+imagelimit.PictureBytes(100, 200); got != want {
		t.Fatalf("reservation did not shrink: %d != %d", got, want)
	}
	if _, err := slot.FromPixels(image.NewRGBA(image.Rect(0, 0, 10, 10)), "second"); err == nil {
		t.Fatal("reservation reused")
	}
	p.Close()
	if imagelimit.Pictures.Used() != start {
		t.Fatal("long capture retained budget")
	}
}

func TestPictureSlotRejectsOversizeAndReleasesOnCancellation(t *testing.T) {
	start := imagelimit.Pictures.Used()
	slot, err := ReservePicture(100)
	if err != nil {
		t.Fatal(err)
	}
	defer slot.Close()
	reserved := imagelimit.Pictures.Used()
	if _, err := slot.FromPixels(image.NewRGBA(image.Rect(0, 0, 11, 10)), "oversize"); !errors.Is(err, imagelimit.ErrBudget) {
		t.Fatalf("oversized result accepted: %v", err)
	}
	if imagelimit.Pictures.Used() != reserved {
		t.Fatal("rejected output changed the reservation")
	}
	slot.Close()
	if imagelimit.Pictures.Used() != start {
		t.Fatal("cancelled capture retained budget")
	}
	if p, err := slot.FromPixels(image.NewRGBA(image.Rect(0, 0, 10, 10)), "closed"); err == nil {
		p.Close()
		t.Fatal("closed slot accepted an output")
	}
}
