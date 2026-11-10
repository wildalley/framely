// Package niriipc places the app's small control windows on niri.
//
// niri does not let Wayland clients position their own windows, and
// xwayland-satellite neither scales X11 windows nor honours override-redirect
// positions, so the XWayland toolbar used on GNOME appears tiny in a corner.
// Instead a native window is opened and niri's IPC floats and moves it.
package niriipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// Available reports whether this session runs under niri with IPC.
func Available() bool {
	if os.Getenv("NIRI_SOCKET") == "" {
		return false
	}
	_, err := exec.LookPath("niri")
	return err == nil
}

type window struct {
	ID         uint64 `json:"id"`
	Title      string `json:"title"`
	PID        int    `json:"pid"`
	IsFocused  bool   `json:"is_focused"`
	IsFloating bool   `json:"is_floating"`
}

// run is replaced in tests.
var run = func(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "niri", append([]string{"msg"}, args...)...).Output()
}

func windows(ctx context.Context) ([]window, error) {
	out, err := run(ctx, "--json", "windows")
	if err != nil {
		return nil, err
	}
	var list []window
	return list, json.Unmarshal(out, &list)
}

// Place floats this process's window titled title and moves it to x, y in
// logical pixels of the output's working area, then gives focus back to
// the window that had it, so scrolling or typing continues there. It waits
// up to two seconds for the window to be mapped.
func Place(ctx context.Context, title string, x, y int) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	pid := os.Getpid()
	var previous uint64
	if list, err := windows(ctx); err == nil {
		for _, w := range list {
			if w.IsFocused && !(w.PID == pid && w.Title == title) {
				previous = w.ID
			}
		}
	}
	var target *window
	for deadline := time.Now().Add(2 * time.Second); target == nil; {
		list, err := windows(ctx)
		if err != nil {
			return err
		}
		for i := range list {
			if list[i].PID == pid && list[i].Title == title {
				target = &list[i]
				break
			}
		}
		if target == nil {
			if time.Now().After(deadline) {
				return errors.New("niri 没有报告控制窗口")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	id := strconv.FormatUint(target.ID, 10)
	if !target.IsFloating {
		if _, err := run(ctx, "action", "toggle-window-floating", "--id", id); err != nil {
			return fmt.Errorf("设置浮动失败：%w", err)
		}
	}
	if _, err := run(ctx, "action", "move-floating-window", "--id", id, "-x", strconv.Itoa(x), "-y", strconv.Itoa(y)); err != nil {
		return fmt.Errorf("移动控制窗口失败：%w", err)
	}
	if previous != 0 {
		_, _ = run(ctx, "action", "focus-window", "--id", strconv.FormatUint(previous, 10))
	}
	return nil
}
