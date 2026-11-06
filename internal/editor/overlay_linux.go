//go:build linux

package editor

import (
	"sync"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
)

// GTK, which MyGo has loaded already, places a full-screen window on the
// monitor it is told, on X11 and Wayland alike.
var (
	gtkOnce                      sync.Once
	gtkOK                        bool
	gdkDisplayGetDefault         func() uintptr
	gdkDisplayGetNMonitors       func(d uintptr) int32
	gdkDisplayGetMonitor         func(d uintptr, i int32) uintptr
	gdkMonitorGetGeometry        func(m uintptr, r *gdkRect)
	gdkScreenGetDefault          func() uintptr
	gtkWindowFullscreenOnMonitor func(w, screen uintptr, monitor int32)
)

type gdkRect struct{ X, Y, W, H int32 }

func loadGTK() bool {
	gtkOnce.Do(func() {
		gdk, err := purego.Dlopen("libgdk-3.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		gtk, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&gdkDisplayGetDefault, gdk, "gdk_display_get_default")
		purego.RegisterLibFunc(&gdkDisplayGetNMonitors, gdk, "gdk_display_get_n_monitors")
		purego.RegisterLibFunc(&gdkDisplayGetMonitor, gdk, "gdk_display_get_monitor")
		purego.RegisterLibFunc(&gdkMonitorGetGeometry, gdk, "gdk_monitor_get_geometry")
		purego.RegisterLibFunc(&gdkScreenGetDefault, gdk, "gdk_screen_get_default")
		purego.RegisterLibFunc(&gtkWindowFullscreenOnMonitor, gtk, "gtk_window_fullscreen_on_monitor")
		gtkOK = true
	})
	return gtkOK
}

// fullscreenOnDisplay makes the GtkWindow full screen on the monitor at
// bounds, and reports whether it found one there. Call it on the main
// thread.
func fullscreenOnDisplay(handle uintptr, bounds mygo.Rectangle) bool {
	if handle == 0 || !loadGTK() {
		return false
	}
	display := gdkDisplayGetDefault()
	screen := gdkScreenGetDefault()
	if display == 0 || screen == 0 {
		return false
	}
	for i := range gdkDisplayGetNMonitors(display) {
		var r gdkRect
		gdkMonitorGetGeometry(gdkDisplayGetMonitor(display, i), &r)
		if int(r.X) == bounds.X && int(r.Y) == bounds.Y && int(r.W) == bounds.Width && int(r.H) == bounds.Height {
			gtkWindowFullscreenOnMonitor(handle, screen, i)
			return true
		}
	}
	return false
}
