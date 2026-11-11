package desktop

import (
	"context"
	"errors"
	"log"
	"math"
	"os"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wildalley/framely/internal/brand"
	"github.com/wildalley/framely/internal/capture"
	"github.com/wildalley/framely/internal/editor"
	"github.com/wildalley/framely/internal/niriipc"
)

// barBeside places a w×h bar next to region on a width×height display,
// trying below, right, left, then above, never over the captured pixels.
func barBeside(region ui.Rect, width, height, w, h float32) (ui.Rect, bool) {
	const gap = float32(24)
	x := max(gap, min(region.X+region.W-w, width-w-gap))
	y := max(gap, min(region.Y+region.H-h, height-h-gap))
	for _, r := range []ui.Rect{
		{X: x, Y: region.Y + region.H + gap, W: w, H: h},
		{X: region.X + region.W + gap, Y: y, W: w, H: h},
		{X: region.X - w - gap, Y: y, W: w, H: h},
		{X: x, Y: region.Y - h - gap, W: w, H: h},
	} {
		if r.X >= 0 && r.Y >= 0 && r.X+r.W <= width && r.Y+r.H <= height {
			return r, true
		}
	}
	return ui.Rect{}, false
}

// openControlBar opens a small always-on-top control window at rect, in
// logical pixels of display. Call it on the UI thread; worker runs the niri
// placement off it.
//
//   - niri: a native Wayland window, floated and moved through niri IPC, so
//     it follows output scaling (XWayland windows are not scaled there).
//   - Other Linux desktops: an override-redirect XWayland window, which
//     GNOME lets us position beside the selection.
//   - Windows/macOS: a positioned window excluded from screen capture.
func openControlBar(ctx context.Context, title string, display mygo.Display, rect ui.Rect, view func(*ui.Context), worker func(func())) (*mygo.Window, error) {
	b := display.Bounds
	input := mygo.Rectangle{X: b.X + int(math.Floor(float64(rect.X))), Y: b.Y + int(math.Floor(float64(rect.Y))), Width: int(math.Ceil(float64(rect.W))), Height: int(math.Ceil(float64(rect.H)))}
	options := mygo.WindowOptions{
		Title: title, X: input.X, Y: input.Y, Width: input.Width, Height: input.Height,
		UseContentSize: true, Frameless: true, Hidden: true,
		AlwaysOnTop: true, SkipTaskbar: true, DisableResize: true, DisableShadow: true,
		Content: ui.View(view),
	}
	if runtime.GOOS != "linux" {
		w := mygo.NewWindow(options)
		capture.ExcludeWindow(w.NativeHandle())
		w.ShowInactive()
		return w, nil
	}
	if niriipc.Available() {
		w := mygo.NewWindow(options)
		w.ShowInactive()
		x, y := int(math.Round(float64(rect.X))), int(math.Round(float64(rect.Y)))
		worker(func() {
			if err := niriipc.Place(ctx, title, x, y); err != nil {
				log.Printf("%s: niri control bar %q: %v", brand.Name, title, err)
			}
		})
		return w, nil
	}
	// The XWayland display must not reuse the main Wayland GPU context; a
	// small bar is cheap to draw with Cairo.
	previousGPU, hadGPU := os.LookupEnv("MYGO_GPU")
	_ = os.Setenv("MYGO_GPU", "0")
	defer func() {
		if hadGPU {
			_ = os.Setenv("MYGO_GPU", previousGPU)
		} else {
			_ = os.Unsetenv("MYGO_GPU")
		}
	}()
	w := mygo.NewWindow(options)
	if !editor.ConfigureScrollToolbar(w.NativeHandle(), input) {
		w.Destroy()
		return nil, errors.New("当前桌面无法在选区旁放置控制条")
	}
	w.ShowInactive()
	return w, nil
}
