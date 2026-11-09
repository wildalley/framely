//go:build !linux

package editor

import "github.com/egoist/mygo"

func ConfigureScrollToolbar(uintptr, mygo.Rectangle) bool { return false }
