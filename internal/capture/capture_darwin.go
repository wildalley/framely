//go:build darwin

package capture

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// System runs macOS's own region selector and returns its PNG.
func System(func() []byte) ([]byte, error) {
	path := filepath.Join(os.TempDir(), "screenpin-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".png")
	defer os.Remove(path)
	err := exec.Command("screencapture", "-i", "-x", path).Run()
	data, readErr := os.ReadFile(path)
	if err != nil || readErr != nil || len(data) == 0 {
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			return nil, errors.New("无法启动系统截图: " + err.Error())
		}
		return nil, ErrCancelled
	}
	return data, nil
}
