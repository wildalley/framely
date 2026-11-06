// Package portal speaks the XDG desktop portal protocol used by ScreenPin.
package portal

import "errors"

// AppID keeps screenshot permissions and shortcut assignments from the
// previous version. The installed desktop entry uses the same ID.
const AppID = "io.screenpin.desktop"

var (
	ErrCancelled = errors.New("门户请求已取消")
	ErrDenied    = errors.New("门户拒绝了请求")
)
