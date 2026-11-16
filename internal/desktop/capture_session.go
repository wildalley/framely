package desktop

import (
	"context"
	"image"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/wildalley/framely/internal/capture"
)

// The same source/display resolution is used by recording and long capture.
func displayForStream(source capture.Stream, preferred mygo.Display, displays []mygo.Display) *mygo.Display {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return &preferred
	}
	bounds, _ := capture.StreamDisplayBounds(source)
	return matchStreamDisplay(bounds, capture.StreamSourceType(source), displays)
}

func matchStreamDisplay(bounds image.Rectangle, sourceType uint32, displays []mygo.Display) *mygo.Display {
	if sourceType == 2 {
		return nil
	}
	if !bounds.Empty() {
		for _, display := range displays {
			b := display.Bounds
			if bounds == image.Rect(b.X, b.Y, b.X+b.Width, b.Y+b.Height) {
				return &display
			}
		}
	}
	// A window-only stream must never inherit a monitor crop. A single
	// monitor source is unambiguous even without portal position metadata.
	if sourceType == 1 && len(displays) == 1 {
		return &displays[0]
	}
	return nil
}

func recordingMonitor(display mygo.Display, displays []mygo.Display, monitors []capture.Monitor) int {
	if runtime.GOOS == "darwin" {
		return int(display.ID)
	}
	if runtime.GOOS == "windows" {
		for i, d := range displays {
			if d.ID == display.ID && i < len(monitors) {
				return monitors[i].ID
			}
		}
	}
	for _, monitor := range monitors {
		if monitor.UnderMouse {
			return monitor.ID
		}
	}
	return 0
}

// Wait for chooser/selection animations without delaying cancellation.
func waitForCapture(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
