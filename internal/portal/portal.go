// Package portal speaks the XDG desktop portal protocol used by ScreenPin.
package portal

import "errors"

// AppID keeps screenshot permissions and shortcut assignments from the
// previous version. The installed desktop entry uses the same ID.
const AppID = "io.screenpin.desktop"

var (
	ErrCancelled = errors.New("门户请求已取消")
	// Response 2 is an unspecified failure, including missing backends;
	// the protocol does not say that the user denied permission.
	ErrFailed = errors.New("门户请求失败（权限被拒绝或桌面后端不可用）")
)
