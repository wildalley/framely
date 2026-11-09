//go:build !linux && !windows

package editor

import "github.com/egoist/mygo"

// macOS uses the system selector.
func fullscreenOnDisplay(uintptr, mygo.Rectangle) bool { return false }
