//go:build !windows

package capture

import (
	"context"
	"errors"
	"image"
)

func DesktopPixels(context.Context) (*image.RGBA, error) {
	return nil, errors.New("此平台不使用 Windows 桌面采集")
}
func ExcludeWindow(uintptr) bool { return false }
