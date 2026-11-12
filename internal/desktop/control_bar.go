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
	return openFloatingBar(ctx, title, display, rect, false, nil, view, worker)
}

func openPassiveBar(ctx context.Context, title string, display mygo.Display, rect ui.Rect, view func(*ui.Context), worker func(func())) (*mygo.Window, error) {
	return openFloatingBar(ctx, title, display, rect, true, nil, view, worker)
}

func openFloatingBar(ctx context.Context, title string, display mygo.Display, rect ui.Rect, ignoreMouse bool, hole *mygo.Rectangle, view func(*ui.Context), worker func(func())) (*mygo.Window, error) {
	b := display.Bounds
	input := mygo.Rectangle{X: b.X + int(math.Floor(float64(rect.X))), Y: b.Y + int(math.Floor(float64(rect.Y))), Width: int(math.Ceil(float64(rect.W))), Height: int(math.Ceil(float64(rect.H)))}
	options := mygo.WindowOptions{
		Title: title, X: input.X, Y: input.Y, Width: input.Width, Height: input.Height,
		UseContentSize: true, Frameless: true, Transparent: true, Hidden: true,
		AlwaysOnTop: true, SkipTaskbar: true, DisableResize: true, DisableShadow: true,
		Content: ui.View(view),
	}
	if runtime.GOOS != "linux" {
		w := mygo.NewWindow(options)
		if ignoreMouse {
			w.SetIgnoreMouseEvents(true)
		}
		capture.ExcludeWindow(w.NativeHandle())
		w.ShowInactive()
		return w, nil
	}
	if niriipc.Available() {
		w := mygo.NewWindow(options)
		if ignoreMouse {
			w.SetIgnoreMouseEvents(true)
		}
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
	var configured bool
	if hole != nil {
		configured = editor.ConfigureRegionFrame(w.NativeHandle(), input, *hole)
	} else {
		configured = editor.ConfigureScrollToolbar(w.NativeHandle(), input)
	}
	if !configured {
		w.Destroy()
		return nil, errors.New("当前桌面无法在选区旁放置控制条")
	}
	if ignoreMouse {
		w.SetIgnoreMouseEvents(true)
	}
	w.ShowInactive()
	return w, nil
}

const (
	regionFrameGap   = float32(2)
	regionFrameThick = float32(3)
)

// regionFrameRect returns the outer window rectangle (in display logical
// coordinates) and the inner transparent hole (in window-local coordinates)
// for a selection frame around region. It returns ok=false when region
// covers the full display and leaves no outer margin for a border.
func regionFrameRect(region ui.Rect, width, height float32) (outer, hole ui.Rect, ok bool) {
	if region.W <= 0 || region.H <= 0 || width <= 0 || height <= 0 {
		return ui.Rect{}, ui.Rect{}, false
	}
	ix0 := float32(math.Floor(float64(region.X)))
	iy0 := float32(math.Floor(float64(region.Y)))
	ix1 := float32(math.Ceil(float64(region.X + region.W)))
	iy1 := float32(math.Ceil(float64(region.Y + region.H)))

	hx0 := max(0, ix0-regionFrameGap)
	hy0 := max(0, iy0-regionFrameGap)
	hx1 := min(width, ix1+regionFrameGap)
	hy1 := min(height, iy1+regionFrameGap)

	outset := regionFrameGap + regionFrameThick
	ox0 := max(0, ix0-outset)
	oy0 := max(0, iy0-outset)
	ox1 := min(width, ix1+outset)
	oy1 := min(height, iy1+outset)

	if ox0 >= hx0 && oy0 >= hy0 && ox1 <= hx1 && oy1 <= hy1 {
		return ui.Rect{}, ui.Rect{}, false
	}
	outer = ui.Rect{X: ox0, Y: oy0, W: ox1 - ox0, H: oy1 - oy0}
	hole = ui.Rect{X: hx0 - ox0, Y: hy0 - oy0, W: hx1 - hx0, H: hy1 - hy0}
	return outer, hole, true
}

func openRegionFrame(ctx context.Context, title string, display mygo.Display, region ui.Rect, view func(*ui.Context), worker func(func())) (*mygo.Window, ui.Rect, ui.Rect, bool) {
	b := display.Bounds
	outer, hole, ok := regionFrameRect(region, float32(b.Width), float32(b.Height))
	if !ok {
		return nil, ui.Rect{}, ui.Rect{}, false
	}
	holeRect := mygo.Rectangle{
		X:      int(math.Floor(float64(hole.X))),
		Y:      int(math.Floor(float64(hole.Y))),
		Width:  int(math.Ceil(float64(hole.W))),
		Height: int(math.Ceil(float64(hole.H))),
	}
	w, err := openFloatingBar(ctx, title, display, outer, true, &holeRect, view, worker)
	if err != nil {
		return nil, ui.Rect{}, ui.Rect{}, false
	}
	return w, outer, hole, true
}

// drawRegionFrame paints a crisp selection outline and corner brackets strictly
// outside hole, leaving the captured interior completely transparent.
func drawRegionFrame(c *ui.Context, hole ui.Rect, stroke ui.Color) {
	panelTheme(c)
	c.Root().Background(ui.Transparent)
	ui.Box(c.Key("region-frame")).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
		topH := hole.Y
		botY := hole.Y + hole.H
		botH := r.H - botY
		leftW := hole.X
		rightX := hole.X + hole.W
		rightW := r.W - rightX
		line := stroke.Alpha(0.75)

		if topH > 0 {
			th := min(float32(2), topH)
			lx := max(float32(0), hole.X-th)
			rx := min(r.W, rightX+th)
			p.Fill(ui.Rect{X: r.X + lx, Y: r.Y + hole.Y - th, W: rx - lx, H: th}, line, 0)
		}
		if botH > 0 {
			bh := min(float32(2), botH)
			lx := max(float32(0), hole.X-bh)
			rx := min(r.W, rightX+bh)
			p.Fill(ui.Rect{X: r.X + lx, Y: r.Y + botY, W: rx - lx, H: bh}, line, 0)
		}
		if leftW > 0 {
			lw := min(float32(2), leftW)
			p.Fill(ui.Rect{X: r.X + hole.X - lw, Y: r.Y + hole.Y, W: lw, H: hole.H}, line, 0)
		}
		if rightW > 0 {
			rw := min(float32(2), rightW)
			p.Fill(ui.Rect{X: r.X + rightX, Y: r.Y + hole.Y, W: rw, H: hole.H}, line, 0)
		}

		arm := min(float32(18), min(hole.W, hole.H)/4)
		if topH > 0 {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: min(r.W, hole.X+arm), H: topH}, stroke, 0)
			x0 := max(float32(0), rightX-arm)
			p.Fill(ui.Rect{X: r.X + x0, Y: r.Y, W: r.W - x0, H: topH}, stroke, 0)
		}
		if botH > 0 {
			p.Fill(ui.Rect{X: r.X, Y: r.Y + botY, W: min(r.W, hole.X+arm), H: botH}, stroke, 0)
			x0 := max(float32(0), rightX-arm)
			p.Fill(ui.Rect{X: r.X + x0, Y: r.Y + botY, W: r.W - x0, H: botH}, stroke, 0)
		}
		if leftW > 0 {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: leftW, H: min(r.H, hole.Y+arm)}, stroke, 0)
			y0 := max(float32(0), botY-arm)
			p.Fill(ui.Rect{X: r.X, Y: r.Y + y0, W: leftW, H: r.H - y0}, stroke, 0)
		}
		if rightW > 0 {
			p.Fill(ui.Rect{X: r.X + rightX, Y: r.Y, W: rightW, H: min(r.H, hole.Y+arm)}, stroke, 0)
			y0 := max(float32(0), botY-arm)
			p.Fill(ui.Rect{X: r.X + rightX, Y: r.Y + y0, W: rightW, H: r.H - y0}, stroke, 0)
		}
	})
}
