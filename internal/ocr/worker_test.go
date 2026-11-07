package ocr

import (
	"context"
	"errors"
	"fmt"
	"image"
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
		_, _ = png.Decode(os.Stdin)
		time.Sleep(time.Minute)
		os.Exit(1)
	}
	os.Exit(m.Run())
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

func TestPackagedOfflineWorker(t *testing.T) {
	if os.Getenv("SCREENPIN_OCR_INTEGRATION") != "1" {
		t.Skip("build components and set SCREENPIN_OCR_INTEGRATION=1")
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r, err := Recognize(ctx, img)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"ScreenPin", "你好", "Windows", "Linux"} {
		if !strings.Contains(r.Text(), text) {
			t.Fatalf("missing %q from %s", text, r.Text())
		}
	}
	fmt.Println(r.Text())
}
