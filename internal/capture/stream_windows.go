//go:build windows

package capture

import (
	"context"
	"encoding/json"
	"image"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/wildalley/framely/internal/components"
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
	name := "screenpin-capture"
	if options.Recording != nil {
		name = "screenpin-record"
	}
	helper, err := components.FindProtocol(name, components.FrameProtocol)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, helper, "--monitor", strconv.Itoa(options.Monitor), "--owner-pid", strconv.Itoa(os.Getpid()), "--protocol", "2")
	if options.Recording != nil {
		args, e := options.Recording.Arguments()
		if e != nil {
			cancel()
			return nil, e
		}
		cmd.Args = append(cmd.Args, args...)
	}
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
	s.input = input
	if options.Automatic {
		s.scroll = func(ctx context.Context, p image.Point) error {
			s.commandMu.Lock()
			defer s.commandMu.Unlock()
			_, err := s.sendCommand(ctx, "scroll", p.X, p.Y)
			return err
		}
	}
	return s, nil
}
