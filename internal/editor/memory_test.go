package editor

import (
	"image"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"
	"weak"

	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/config"
)

// This deliberately uses the software renderer. Native GPU/OS allocations
// require a separate desktop run and are not represented by Go heap profiles.
func TestSoftwareImageLifecycleMemoryStress(t *testing.T) {
	if os.Getenv("SCREENPIN_MEMORY_AUDIT") != "1" {
		t.Skip("set SCREENPIN_MEMORY_AUDIT=1 for repeated image/render/close cycles")
	}
	var closed []*App
	round := func() weak.Pointer[byte] {
		pixels := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
		pointer := weak.Make(&pixels.Pix[0])
		picture, err := PictureFromPixels(pixels, "memory audit")
		defer picture.Close()
		if err != nil {
			t.Fatal(err)
		}
		a := New(config.Default())
		a.loadPicture(picture, nil)
		tt := ui.NewTester(a.view, 640, 480)
		_ = tt.Image()
		a.Close()
		tt.Frame()
		closed = append(closed, a)
		return pointer
	}
	for i := 0; i < 10; i++ {
		round()
	}
	runtime.GC()
	var start, end runtime.MemStats
	runtime.ReadMemStats(&start)
	for batch := 0; batch < 4; batch++ {
		var pointers []weak.Pointer[byte]
		for i := 0; i < 20; i++ {
			pointers = append(pointers, round())
		}
		runtime.GC()
		runtime.ReadMemStats(&end)
		for _, pointer := range pointers {
			if pointer.Value() != nil {
				t.Fatal("closed editor retained the screenshot's pixel buffer")
			}
		}
		t.Logf("after %d measured cycles: heap %.2f MiB, objects %d, goroutines %d", (batch+1)*20, float64(end.HeapAlloc)/(1<<20), end.HeapObjects, runtime.NumGoroutine())
	}
	if end.HeapAlloc > start.HeapAlloc+8<<20 {
		t.Fatalf("post-GC heap grew by %.2f MiB", float64(end.HeapAlloc-start.HeapAlloc)/(1<<20))
	}
	if directory := os.Getenv("SCREENPIN_MEMORY_PROFILE_DIR"); directory != "" {
		f, err := os.Create(filepath.Join(directory, "editor-heap.pprof"))
		if err != nil {
			t.Fatal(err)
		}
		if err := pprof.WriteHeapProfile(f); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	runtime.KeepAlive(closed)
}
