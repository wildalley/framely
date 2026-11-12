//go:build !linux

package editor

import "github.com/egoist/mygo"

func ConfigureScrollToolbar(uintptr, mygo.Rectangle) bool               { return false }
func ConfigureRegionFrame(uintptr, mygo.Rectangle, mygo.Rectangle) bool { return false }
