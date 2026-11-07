//go:build !linux && !windows

package capture

import (
	"context"
	"errors"
)

func StreamMonitors(context.Context) ([]Monitor, error) { return nil, nil }
func AutomaticScrollAvailable(context.Context) bool     { return false }
func OpenStream(context.Context, StreamOptions) (Stream, error) {
	return nil, errors.New("长截图目前支持 Windows 和 Linux")
}
