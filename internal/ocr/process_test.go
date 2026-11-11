package ocr

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func serveTestWorker() {
	for {
		var header [16]byte
		if _, err := io.ReadFull(os.Stdin, header[:]); err != nil {
			return
		}
		if string(header[:4]) != "SPO2" {
			return
		}
		if _, err := io.CopyN(io.Discard, os.Stdin, int64(binary.LittleEndian.Uint32(header[12:]))); err != nil {
			return
		}
		if os.Getenv("SCREENPIN_OCR_HELPER_INVALID") == "1" {
			_, _ = os.Stdout.Write([]byte{255, 255, 255, 255})
			return
		}
		line := Line{Text: fmt.Sprintf("worker-%d", os.Getpid()), Confidence: 1, Box: [4]Point{{0, 0}, {2, 0}, {2, 2}, {0, 2}}}
		for _, msg := range []any{map[string]any{"type": "progress", "completed": 1, "total": 1, "lines": []Line{line}}, map[string]any{"type": "result", "model": "test", "lines": []Line{line}}} {
			data, _ := json.Marshal(msg)
			var n [4]byte
			binary.LittleEndian.PutUint32(n[:], uint32(len(data)))
			_, _ = os.Stdout.Write(n[:])
			_, _ = os.Stdout.Write(data)
		}
	}
}

func installTestWorker(t *testing.T) {
	t.Helper()
	Close()
	dir := t.TempDir()
	// Cleanups run in reverse registration order. Reap the reusable worker
	// before TempDir removes its executable, which Windows keeps locked.
	t.Cleanup(Close)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "framely-ocr"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	in, err := os.Open(exe)
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
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	t.Setenv("SCREENPIN_COMPONENTS_DIR", dir)
	t.Setenv("SCREENPIN_OCR_HELPER_TEST", "server")
}

func TestWorkerReusesModelAndExpiresIdleProcess(t *testing.T) {
	installTestWorker(t)
	old := sharedWorker.idleTimeout
	sharedWorker.idleTimeout = time.Minute
	t.Cleanup(func() { Close(); sharedWorker.idleTimeout = old })
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	progress := 0
	a, err := RecognizePrepared(context.Background(), func(context.Context) (*image.RGBA, error) { return img, nil }, func(p Progress) {
		if p.Completed > 0 {
			progress++
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Recognize(context.Background(), img)
	if err != nil || a.Text() != b.Text() || progress != 1 {
		t.Fatalf("reuse: %v %v %v progress=%d", a, b, err, progress)
	}
	sharedWorker.mu.Lock()
	p := sharedWorker.process
	sharedWorker.timer.Stop()
	sharedWorker.idleTimeout = time.Millisecond
	sharedWorker.mu.Unlock()
	sharedWorker.release(p, true)
	select {
	case <-p.stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("idle worker was retained")
	}
	c, err := Recognize(context.Background(), img)
	if err != nil || c.Text() == a.Text() {
		t.Fatalf("idle worker not restarted: %v %v", c, err)
	}
}

func TestQueuedCancellationDoesNotAllocateImage(t *testing.T) {
	workerSlots <- struct{}{}
	defer func() { <-workerSlots }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RecognizePrepared(ctx, func(context.Context) (*image.RGBA, error) { t.Fatal("prepared cancelled image"); return nil, nil }, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMalformedWorkerIsKilledAndNextRequestRestarts(t *testing.T) {
	installTestWorker(t)
	t.Setenv("SCREENPIN_OCR_HELPER_INVALID", "1")
	_, err := Recognize(context.Background(), image.NewRGBA(image.Rect(0, 0, 10, 10)))
	if err == nil {
		t.Fatal("accepted oversized response")
	}
	t.Setenv("SCREENPIN_OCR_HELPER_INVALID", "")
	if _, err = Recognize(context.Background(), image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
}

func TestRGBTransportHonorsStrideAndTransparentPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 5, 3))
	img.SetRGBA(2, 1, color.RGBA{R: 128, A: 128})
	img.SetRGBA(3, 1, color.RGBA{G: 255, A: 255})
	var buf bytes.Buffer
	if err := writePixels(context.Background(), &buf, img.SubImage(image.Rect(2, 1, 4, 2)).(*image.RGBA)); err != nil {
		t.Fatal(err)
	}
	if got := buf.Bytes()[16:]; !bytes.Equal(got, []byte{255, 127, 127, 0, 255, 0}) {
		t.Fatalf("RGB=%v", got)
	}
}

func TestTableCellsRemainSeparateAndTileFragmentsJoin(t *testing.T) {
	line := func(text string, x, right float64, tile int) Line {
		return Line{Text: text, Confidence: 1, Tile: tile, Box: [4]Point{{x, 0}, {right, 0}, {right, 20}, {x, 20}}}
	}
	cells := MergeLines([]Line{line("姓名", 0, 40, 1), line("金额", 100, 140, 1), line("电话", 240, 280, 1)})
	if len(cells) != 3 {
		t.Fatalf("merged table cells: %+v", cells)
	}
	fragments := MergeLines([]Line{line("Hello ScreenPin", 0, 150, 1), line("ScreenPin world", 70, 230, 2)})
	if len(fragments) != 1 || fragments[0].Text != "Hello ScreenPin world" {
		t.Fatal(fragments)
	}
	if len(MergeLines([]Line{line("11", 0, 40, 1), line("11", 100, 140, 2)})) != 2 {
		t.Fatal("deduplicated unrelated repeated cells")
	}
}

func TestOverlapHandlesRepeatedUnicodeAndKeepsSingleCharacterMatches(t *testing.T) {
	for _, tc := range [][3]string{
		{"你好世界", "世界你好", "你好"},
		{"ababa", "ababac", "c"},
		{"aaa", "aa", ""},
		{"张三", "三个人", "三个人"},
		{"", "", ""},
		{strings.Repeat("甲", 100000) + "乙", strings.Repeat("甲", 100000) + "丙", strings.Repeat("甲", 100000) + "丙"},
	} {
		if got := trimOverlap(tc[0], tc[1]); got != tc[2] {
			t.Fatalf("overlap returned %q", got[:min(80, len(got))])
		}
	}
}

func TestResultTextBudgetRejectsExcessiveText(t *testing.T) {
	r := Result{}
	for range 22 {
		r.Lines = append(r.Lines, Line{Text: strings.Repeat("x", 100000)})
	}
	if validate(&r, 100, 100) == nil {
		t.Fatal("accepted more than 2 MiB of recognized text")
	}
}

func TestCancellationAfterWorkerStartupRestartsNextRequest(t *testing.T) {
	installTestWorker(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var process *workerProcess
	_, err := RecognizePrepared(ctx, func(context.Context) (*image.RGBA, error) { return image.NewRGBA(image.Rect(0, 0, 10, 10)), nil }, func(p Progress) {
		if p.Stage == "recognizing" {
			sharedWorker.mu.Lock()
			process = sharedWorker.process
			sharedWorker.mu.Unlock()
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || process == nil {
		t.Fatalf("cancel after startup: %v", err)
	}
	select {
	case <-process.stopped:
	default:
		t.Fatal("cancelled process still running")
	}
	if _, err := Recognize(context.Background(), image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal("next request failed", err)
	}
}
