//go:build darwin

package capture

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/wildalley/screenpin/internal/imagelimit"
)

// System runs macOS's own region selector and returns its PNG.
func System(func() []byte) ([]byte, error) {
	return SystemContext(context.Background(), nil)
}

func SystemContext(ctx context.Context, _ func() []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := filepath.Join(os.TempDir(), "screenpin-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".png")
	defer os.Remove(path)
	err := exec.CommandContext(ctx, "screencapture", "-i", "-x", path).Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	data, readErr := imagelimit.ReadFile(path)
	if errors.Is(readErr, imagelimit.ErrEncodedSize) {
		return nil, readErr
	}
	if err != nil || readErr != nil || len(data) == 0 {
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			return nil, errors.New("无法启动系统截图: " + err.Error())
		}
		return nil, ErrCancelled
	}
	return data, nil
}
