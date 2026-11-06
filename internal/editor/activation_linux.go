//go:build linux

package editor

import (
	"sync"

	"github.com/ebitengine/purego"
)

var activation struct {
	once        sync.Once
	displayType func() uintptr
	isA         func(uintptr, uintptr) bool
	setID       func(uintptr, *byte)
}

func withActivationToken(token string, fn func()) {
	if token == "" || !loadGTK() {
		fn()
		return
	}
	activation.once.Do(func() {
		gdk, err := purego.Dlopen("libgdk-3.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		if _, err := purego.Dlsym(gdk, "gdk_wayland_display_get_type"); err != nil {
			return
		}
		purego.RegisterLibFunc(&activation.displayType, gdk, "gdk_wayland_display_get_type")
		purego.RegisterLibFunc(&activation.isA, gdk, "g_type_check_instance_is_a")
		purego.RegisterLibFunc(&activation.setID, gdk, "gdk_wayland_display_set_startup_notification_id")
	})
	display := gdkDisplayGetDefault()
	if activation.setID == nil || display == 0 || !activation.isA(display, activation.displayType()) {
		fn()
		return
	}
	text := append([]byte(token), 0)
	activation.setID(display, &text[0])
	defer activation.setID(display, nil)
	fn()
}
