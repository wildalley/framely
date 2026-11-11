package assets

import (
	_ "embed"
	"runtime"
)

// TrayIcon is the app icon's tile, for the Linux and Windows trays.
//
//go:embed tray-icon.png
var TrayIcon []byte

// TrayTemplate is black silhouette that macOS tints for the menu bar and
// Windows shows light on a dark taskbar.
//
//go:embed tray-template.png
var TrayTemplate []byte

// TrayRecording marks a recording in progress with a red dot.
//
//go:embed tray-recording.png
var TrayRecording []byte

// Mark is the product glyph (folded paper F) for in-app headers.
//
//go:embed mark.svg
var Mark []byte

// IdleTrayIcon returns the platform's resting tray icon and whether it is a
// template image.
func IdleTrayIcon() ([]byte, bool) {
	if runtime.GOOS == "darwin" {
		return TrayTemplate, true // The menu bar tints a monochrome icon.
	}
	return TrayIcon, false
}
