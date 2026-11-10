package ocr

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("SCREENPIN_OCR_HELPER_TEST") == "blocked" {
		var header [16]byte
		_, _ = io.ReadFull(os.Stdin, header[:])
		_, _ = io.CopyN(io.Discard, os.Stdin, int64(binary.LittleEndian.Uint32(header[12:])))
		time.Sleep(time.Minute)
		os.Exit(1)
	}
	if os.Getenv("SCREENPIN_OCR_HELPER_TEST") == "server" {
		serveTestWorker()
		os.Exit(0)
	}
	code := m.Run()
	Close()
	os.Exit(code)
}

func TestWorkerCancellationStopsChild(t *testing.T) {
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "screenpin-ocr"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	in, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	t.Setenv("SCREENPIN_COMPONENTS_DIR", dir)
	t.Setenv("SCREENPIN_OCR_HELPER_TEST", "blocked")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = Recognize(ctx, image.NewRGBA(image.Rect(0, 0, 100, 100)))
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("worker was not cancelled promptly: %v (%v)", err, time.Since(start))
	}
}

func TestQueuedOCRCanBeCancelledWithoutStartingAnotherWorker(t *testing.T) {
	dir := t.TempDir()
	name := "screenpin-ocr"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	// Intentionally not executable: reaching process startup is a failure.
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCREENPIN_COMPONENTS_DIR", dir)
	workerSlots <- struct{}{}
	defer func() { <-workerSlots }()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := Recognize(ctx, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued OCR ignored cancellation or started a second worker: %v", err)
	}
}

func TestPackagedOfflineWorker(t *testing.T) {
	if os.Getenv("SCREENPIN_OCR_INTEGRATION") != "1" {
		t.Skip("build components and set SCREENPIN_OCR_INTEGRATION=1")
	}
	Close()
	t.Cleanup(Close)
	if os.Getenv("SCREENPIN_COMPONENTS_DIR") == "" {
		root, err := filepath.Abs(filepath.Join("..", "..", "resources", runtime.GOOS+"-"+runtime.GOARCH, "workers"))
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("SCREENPIN_COMPONENTS_DIR", root)
	}
	f, err := os.Open("testdata/bilingual.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decoded, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(decoded.Bounds())
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.Set(x, y, decoded.At(x, y))
		}
	}
	tall := image.NewRGBA(image.Rect(0, 0, 2000, 2200))
	draw.Draw(tall, tall.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	// Exercise both tile axes and text near overlapping tile boundaries.
	for y := 0; y+img.Rect.Dy() <= tall.Rect.Dy(); y += 330 {
		draw.Draw(tall, image.Rect(750, y, 1750, y+img.Rect.Dy()), img, image.Point{}, draw.Src)
	}
	for _, test := range []struct {
		name string
		img  *image.RGBA
	}{{"bilingual", img}, {"bilingual-warm", img}, {"tiled-long-image", tall}} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			start := time.Now()
			batches := 0
			r, err := RecognizePrepared(ctx, func(context.Context) (*image.RGBA, error) { return test.img, nil }, func(p Progress) {
				if p.Completed > 0 {
					batches++
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{"ScreenPin", "你好", "Windows", "Linux"} {
				if !strings.Contains(r.Text(), text) {
					t.Fatalf("missing %q from %s", text, r.Text())
				}
			}
			if batches == 0 {
				t.Fatal("no streamed progress")
			}
			t.Logf("recognized %d rows in %v, %d progress batches", len(r.Lines), time.Since(start), batches)
			if test.name == "bilingual" {
				fmt.Println(r.Text())
			}
		})
	}
}
