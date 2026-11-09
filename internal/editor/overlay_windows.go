//go:build windows

package editor

import (
	"syscall"

	"github.com/egoist/mygo"
)

// Position in physical coordinates, so crossing mixed-DPI monitors doesn't
// apply the old window's DPI to the new display's bounds.
func fullscreenOnDisplay(handle uintptr, bounds mygo.Rectangle) bool {
	for _, d := range mygo.Screen.Displays() {
		if d.Bounds != bounds {
			continue
		}
		r := physicalDisplay(d)
		ok, _, _ := syscall.NewLazyDLL("user32.dll").NewProc("SetWindowPos").Call(handle, ^uintptr(0), uintptr(r.Min.X), uintptr(r.Min.Y), uintptr(r.Dx()), uintptr(r.Dy()), 0x40)
		return ok != 0
	}
	return false
}
