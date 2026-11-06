//go:build !linux

package editor

import "github.com/egoist/mygo"

// fullscreenOnDisplay is for Linux's region picker; macOS and Windows use
// the system's selector.
func fullscreenOnDisplay(uintptr, mygo.Rectangle) bool { return false }
