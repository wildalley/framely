package capture

import (
	"context"
	"errors"
	"github.com/wildalley/screenpin/internal/components"
	"os/exec"
	"strconv"
)

func StreamMonitors(context.Context) ([]Monitor, error) { return nil, nil }
func AutomaticScrollAvailable(context.Context) bool     { return false }
func OpenStream(parent context.Context, options StreamOptions) (Stream, error) {
	if options.Recording == nil {
		return nil, errors.New("macOS 当前支持录屏，暂不支持长截图")
	}
	helper, err := components.FindProtocol("screenpin-record", components.FrameProtocol)
	if err != nil {
		return nil, err
	}
	args, err := options.Recording.Arguments()
	if err != nil {
		return nil, err
	}
	args = append(args, "--protocol", "2", "--monitor", strconv.Itoa(options.Monitor))
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, helper, args...)
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
	s.sourceType = 1
	return s, nil
}
