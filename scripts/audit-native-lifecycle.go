//go:build ignore

// Run on a graphical Linux session: go run scripts/audit-native-lifecycle.go.
// Shows generated test pixels in small inactive windows; never captures the desktop.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type sample struct {
	Cycles     int     `json:"cycles"`
	HeapMiB    float64 `json:"heap_mib"`
	RSSKiB     uint64  `json:"rss_kib"`
	FDs        int     `json:"fds"`
	Goroutines int     `json:"goroutines"`
}

func measure(cycles int) sample {
	runtime.GC()
	debug.FreeOSMemory()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	var virtual, resident uint64
	data, _ := os.ReadFile("/proc/self/statm")
	fmt.Sscanf(string(data), "%d %d", &virtual, &resident)
	fds, _ := os.ReadDir("/proc/self/fd")
	return sample{Cycles: cycles, HeapMiB: float64(mem.HeapAlloc) / (1 << 20), RSSKiB: resident * uint64(os.Getpagesize()) / 1024, FDs: len(fds), Goroutines: runtime.NumGoroutine()}
}

func main() {
	cycles := flag.Int("cycles", 60, "number of window creation/destruction cycles")
	output := flag.String("output", "", "optional JSON report path")
	flag.Parse()
	if runtime.GOOS != "linux" {
		fmt.Fprintln(os.Stderr, "This RSS/FD probe requires Linux")
		os.Exit(2)
	}
	if *cycles < 1 || *cycles > 1000 {
		fmt.Fprintln(os.Stderr, "cycles must be between 1 and 1000")
		os.Exit(2)
	}
	runtime.LockOSThread()
	mygo.App.SetName("Framely resource audit")
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		go func() {
			samples := []sample{}
			for i := 1; i <= *cycles; i++ {
				var window *mygo.Window
				mygo.RunOnMain(func() {
					pixels := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
					for j := 3; j < len(pixels.Pix); j += 4 {
						pixels.Pix[j] = 255
					}
					bitmap := ui.NewBitmap(pixels)
					window = mygo.NewWindow(mygo.WindowOptions{Title: "Framely resource audit", Width: 160, Height: 100, UseContentSize: true, SkipTaskbar: true, Hidden: true, Content: ui.View(func(c *ui.Context) {
						ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) { p.ImageRegion(bitmap, ui.Rect{W: 1920, H: 1080}, r, ui.ImageOptions{}) })
					})})
					window.ShowInactive()
				})
				time.Sleep(70 * time.Millisecond)
				mygo.RunOnMain(func() { window.Destroy() })
				window = nil
				if i%10 == 0 || i == *cycles {
					time.Sleep(150 * time.Millisecond)
					s := measure(i)
					samples = append(samples, s)
					data, _ := json.Marshal(s)
					fmt.Println(string(data))
				}
			}
			data, _ := json.MarshalIndent(samples, "", "  ")
			if *output != "" {
				if err := os.MkdirAll(filepath.Dir(*output), 0755); err != nil {
					fmt.Fprintln(os.Stderr, err)
				} else if err := os.WriteFile(*output, append(data, '\n'), 0600); err != nil {
					fmt.Fprintln(os.Stderr, err)
				}
			}
			mygo.RunOnMain(func() { mygo.App.Quit() })
		}()
	})
	if err := mygo.App.Run(); err != nil {
		panic(err)
	}
}
