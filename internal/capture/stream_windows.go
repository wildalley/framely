//go:build windows

package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/wildalley/screenpin/internal/components"
)

func StreamMonitors(ctx context.Context) ([]Monitor, error) {
	helper, err := components.Find("screenpin-capture")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, helper, "--list")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	data, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var monitors []Monitor
	err = json.Unmarshal(data, &monitors)
	return monitors, err
}
func AutomaticScrollAvailable(context.Context) bool { return true }

func OpenStream(parent context.Context, options StreamOptions) (Stream, error) {
	helper, err := components.Find("screenpin-capture")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, helper, "--monitor", strconv.Itoa(options.Monitor), "--owner-pid", strconv.Itoa(os.Getpid()))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	s, err := startStream(ctx, cmd, cancel, func() { _ = input.Close() })
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	var id uint32
	if options.Automatic {
		s.scroll = func(ctx context.Context, p image.Point) error {
			mu.Lock()
			defer mu.Unlock()
			id++
			ctx, done := context.WithTimeout(ctx, 2*time.Second)
			defer done()
			if _, err := fmt.Fprintf(input, "scroll %d %d %d\n", id, p.X, p.Y); err != nil {
				return err
			}
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-s.done:
					return s.err
				case reply := <-s.control:
					if reply.id == id {
						return reply.err
					}
				}
			}
		}
	}
	return s, nil
}
