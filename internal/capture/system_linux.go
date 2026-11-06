//go:build linux

package capture

import "context"

// System asks the portal for the desktop's own region selector.
func System(func() []byte) ([]byte, error) { return Screenshot(true) }

func SystemContext(ctx context.Context, _ func() []byte) ([]byte, error) {
	return ScreenshotContext(ctx, true)
}
