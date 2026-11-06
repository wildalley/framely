//go:build !linux

package capture

import "errors"

// ScreenshotAccess is about the Linux portal; elsewhere the system's own
// selector asks for what it needs.
func ScreenshotAccess() Access { return AccessUnsupported }

// Screenshot is the Linux portal's; other systems use System.
func Screenshot(bool) ([]byte, error) { return nil, errors.New("此平台不使用截图门户") }
