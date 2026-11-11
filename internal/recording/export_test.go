package recording

import (
	"bytes"
	"context"
	"errors"
	"image/gif"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/wildalley/framely/internal/components"
)

func TestGIFRejectsNonfiniteAndUnboundedInputs(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1} {
		for _, o := range []GIFOptions{{Start: value, Duration: 1, FPS: 15, Width: 640}, {Duration: value, FPS: 15, Width: 640}} {
			if o.Validate() == nil {
				t.Fatalf("accepted %+v", o)
			}
		}
	}
	if (GIFOptions{Duration: 61, FPS: 60, Width: 4096}).Validate() == nil {
		t.Fatal("accepted oversized GIF")
	}
}

func TestFailedOrCanceledSavePreservesDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mp4")
	target := filepath.Join(dir, "target.mp4")
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SaveCopy(ctx, source, target); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	staged, err := Stage(target)
	if err != nil {
		t.Fatal(err)
	}
	if err = Publish(staged, target); err == nil {
		t.Fatal("published empty video")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "original" {
		t.Fatal("destination was modified on failure")
	}
	if err = SaveCopy(context.Background(), source, target); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(target)
	if string(data) != "new" {
		t.Fatal("successful replacement failed")
	}
}

// Uses only the shipped offline codecs: catches missing demuxers, filters and
// protocols that a full system FFmpeg would silently mask during development.
func TestPackagedMediaExports(t *testing.T) {
	if os.Getenv("SCREENPIN_MEDIA_INTEGRATION") != "1" {
		t.Skip("set SCREENPIN_MEDIA_INTEGRATION=1 with built media workers")
	}
	ffmpeg, err := components.Find("screenpin-ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	source := filepath.Join(dir, "视频 带空格.mp4")
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-f", "rawvideo", "-pixel_format", "rgba", "-video_size", "320x180", "-framerate", "60", "-i", "pipe:0", "-an", "-c:v", "mpeg4", "-f", "mp4", source)
	var input bytes.Buffer
	frame := make([]byte, 320*180*4)
	for n := 0; n < 120; n++ {
		for y := 0; y < 180; y++ {
			for x := 0; x < 320; x++ {
				i := (y*320 + x) * 4
				frame[i] = byte(x + n)
				frame[i+1] = byte(y + n*2)
				frame[i+2] = byte(n * 2)
				frame[i+3] = 255
			}
		}
		input.Write(frame)
	}
	cmd.Stdin = &input
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate fixture: %v\n%s", err, out)
	}
	dest := filepath.Join(dir, "export.gif")
	var progress []float64
	if length, err := Duration(source); err != nil || length < 1.9 || length > 2.1 {
		t.Fatalf("fixture duration %v %v", length, err)
	}
	if err := ExportGIF(ctx, source, dest, GIFOptions{Start: .5, Duration: 1, FPS: 15, Width: 160, Progress: func(p float64) { progress = append(progress, p) }}); err != nil {
		t.Fatal(err)
	}
	if len(progress) < 2 || progress[0] != 0 || progress[len(progress)-1] != 1 {
		t.Fatalf("progress: %v", progress)
	}
	for i := 1; i < len(progress); i++ {
		if progress[i] < progress[i-1] {
			t.Fatalf("progress went backwards: %v", progress)
		}
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	animation, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(animation.Image) != 15 || animation.Config.Width != 160 || animation.Config.Height != 90 {
		t.Fatalf("GIF output: %d frames, %+v", len(animation.Image), animation.Config)
	}
	delay := 0
	for _, d := range animation.Delay {
		delay += d
	}
	if delay < 95 || delay > 105 {
		t.Fatalf("GIF duration: %d cs", delay)
	}
	thumb := filepath.Join(dir, "thumbnail.bmp")
	if err := Thumbnail(ctx, source, thumb); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(thumb)
	if err != nil || len(data) < 54 || string(data[:2]) != "BM" {
		t.Fatal("missing bitmap thumbnail", err)
	}
	original, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := ExportGIF(canceled, source, dest, GIFOptions{Duration: 1, FPS: 15, Width: 160}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(dest)
	if !bytes.Equal(original, data) {
		t.Fatal("cancel destroyed prior GIF")
	}
	if err := ExportGIF(ctx, source, dest, GIFOptions{Start: 50, Duration: 1, FPS: 15, Width: 160}); err == nil {
		t.Fatal("exported empty range")
	}
	data, _ = os.ReadFile(dest)
	if !bytes.Equal(original, data) {
		t.Fatal("invalid range destroyed prior GIF")
	}
}
