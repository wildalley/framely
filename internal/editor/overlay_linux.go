//go:build linux

package editor

import (
	"os"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
)

// GTK, which MyGo has loaded already, places a full-screen window on the
// monitor it is told, on X11 and Wayland alike.
var (
	gtkOnce                          sync.Once
	gtkOK                            bool
	gdkDisplayGetDefault             func() uintptr
	gdkDisplayGetNMonitors           func(d uintptr) int32
	gdkDisplayGetMonitor             func(d uintptr, i int32) uintptr
	gdkMonitorGetGeometry            func(m uintptr, r *gdkRect)
	gdkScreenGetDefault              func() uintptr
	gtkWindowFullscreenOnMonitor     func(w, screen uintptr, monitor int32)
	gtkWindowSetFocusOnMap           func(w uintptr, focus bool)
	gdkDisplayOpen                   func(name string) uintptr
	gdkDisplayGetDefaultScreen       func(d uintptr) uintptr
	gdkScreenGetRGBAVisual           func(screen uintptr) uintptr
	gtkWindowSetScreen               func(w, screen uintptr)
	gtkWindowMove                    func(w uintptr, x, y int32)
	gtkWindowResize                  func(w uintptr, width, height int32)
	gtkWindowSetKeepAbove            func(w uintptr, above bool)
	gtkWidgetSetVisual               func(w, visual uintptr)
	gtkWidgetSetAppPaintable         func(w uintptr, paintable bool)
	gtkWidgetUnrealize               func(w uintptr)
	gtkWidgetRealize                 func(w uintptr)
	gtkWidgetGetWindow               func(w uintptr) uintptr
	gdkWindowSetOverrideRedirect     func(w uintptr, override bool)
	gdkWindowMoveResize              func(w uintptr, x, y, width, height int32)
	gdkWindowShapeCombineRegion      func(w, region uintptr, x, y int32)
	gdkWindowInputShapeCombineRegion func(w, region uintptr, x, y int32)
	cairoRegionCreateRectangle       func(r *gdkRect) uintptr
	cairoRegionSubtractRectangle     func(dst uintptr, r *gdkRect) int32
	cairoRegionDestroy               func(r uintptr)
	toolbarDisplay                   uintptr
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
		purego.RegisterLibFunc(&gtkWindowSetFocusOnMap, gtk, "gtk_window_set_focus_on_map")
		purego.RegisterLibFunc(&gdkDisplayOpen, gdk, "gdk_display_open")
		purego.RegisterLibFunc(&gdkDisplayGetDefaultScreen, gdk, "gdk_display_get_default_screen")
		purego.RegisterLibFunc(&gdkScreenGetRGBAVisual, gdk, "gdk_screen_get_rgba_visual")
		purego.RegisterLibFunc(&gtkWindowSetScreen, gtk, "gtk_window_set_screen")
		purego.RegisterLibFunc(&gtkWindowMove, gtk, "gtk_window_move")
		purego.RegisterLibFunc(&gtkWindowResize, gtk, "gtk_window_resize")
		purego.RegisterLibFunc(&gtkWindowSetKeepAbove, gtk, "gtk_window_set_keep_above")
		purego.RegisterLibFunc(&gtkWidgetSetVisual, gtk, "gtk_widget_set_visual")
		purego.RegisterLibFunc(&gtkWidgetSetAppPaintable, gtk, "gtk_widget_set_app_paintable")
		purego.RegisterLibFunc(&gtkWidgetUnrealize, gtk, "gtk_widget_unrealize")
		purego.RegisterLibFunc(&gtkWidgetRealize, gtk, "gtk_widget_realize")
		purego.RegisterLibFunc(&gtkWidgetGetWindow, gtk, "gtk_widget_get_window")
		purego.RegisterLibFunc(&gdkWindowSetOverrideRedirect, gdk, "gdk_window_set_override_redirect")
		purego.RegisterLibFunc(&gdkWindowMoveResize, gdk, "gdk_window_move_resize")
		purego.RegisterLibFunc(&gdkWindowShapeCombineRegion, gdk, "gdk_window_shape_combine_region")
		purego.RegisterLibFunc(&gdkWindowInputShapeCombineRegion, gdk, "gdk_window_input_shape_combine_region")
		if cairo, err := purego.Dlopen("libcairo.so.2", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			purego.RegisterLibFunc(&cairoRegionCreateRectangle, cairo, "cairo_region_create_rectangle")
			purego.RegisterLibFunc(&cairoRegionSubtractRectangle, cairo, "cairo_region_subtract_rectangle")
			purego.RegisterLibFunc(&cairoRegionDestroy, cairo, "cairo_region_destroy")
		}
		gtkOK = true
	})
	return gtkOK
}

// ConfigureScrollToolbar uses an X11/XWayland connection for this small window
// only: GNOME's Wayland protocol does not allow apps to position a toolbar or
// keep it above another app. Desktop capture continues through the portal.
// Call before showing/realizing the toolbar, on the UI thread.
func ConfigureScrollToolbar(handle uintptr, bounds mygo.Rectangle) bool {
	if handle == 0 || !openToolbarDisplay() {
		return false
	}
	if gtkWidgetGetWindow(handle) != 0 {
		gtkWidgetUnrealize(handle)
	}
	screen := gdkDisplayGetDefaultScreen(toolbarDisplay)
	gtkWindowSetScreen(handle, screen)
	if visual := gdkScreenGetRGBAVisual(screen); visual != 0 {
		gtkWidgetSetVisual(handle, visual)
	}
	gtkWidgetSetAppPaintable(handle, true)
	gtkWindowSetFocusOnMap(handle, false)
	gtkWindowSetKeepAbove(handle, true)
	gtkWindowMove(handle, int32(bounds.X), int32(bounds.Y))
	gtkWindowResize(handle, int32(bounds.Width), int32(bounds.Height))
	// This is an opaque, small input window. Bypass Mutter's initial
	// placement before mapping it, so it cannot jump to a remembered or
	// compositor-chosen position, or acquire a shadow inside the capture.
	gtkWidgetRealize(handle)
	window := gtkWidgetGetWindow(handle)
	if window == 0 {
		return false
	}
	gdkWindowSetOverrideRedirect(window, true)
	gdkWindowMoveResize(window, int32(bounds.X), int32(bounds.Y), int32(bounds.Width), int32(bounds.Height))
	return true
}

// ConfigureRegionFrame places a click-through selection frame around a captured
// region and cuts out the inner hole from the window's bounding shape so
// neither the compositor nor the capture stream touches the interior.
func ConfigureRegionFrame(handle uintptr, bounds, hole mygo.Rectangle) bool {
	if !ConfigureScrollToolbar(handle, bounds) {
		return false
	}
	window := gtkWidgetGetWindow(handle)
	if window == 0 {
		return false
	}
	if cairoRegionCreateRectangle != nil && cairoRegionSubtractRectangle != nil && cairoRegionDestroy != nil && gdkWindowShapeCombineRegion != nil {
		outer := gdkRect{X: 0, Y: 0, W: int32(bounds.Width), H: int32(bounds.Height)}
		if shape := cairoRegionCreateRectangle(&outer); shape != 0 {
			if hole.Width > 0 && hole.Height > 0 {
				inner := gdkRect{X: int32(hole.X), Y: int32(hole.Y), W: int32(hole.Width), H: int32(hole.Height)}
				cairoRegionSubtractRectangle(shape, &inner)
			}
			gdkWindowShapeCombineRegion(window, shape, 0, 0)
			cairoRegionDestroy(shape)
		}
	}
	if cairoRegionCreateRectangle != nil && cairoRegionDestroy != nil && gdkWindowInputShapeCombineRegion != nil {
		if empty := cairoRegionCreateRectangle(&gdkRect{}); empty != 0 {
			gdkWindowInputShapeCombineRegion(window, empty, 0, 0)
			cairoRegionDestroy(empty)
		}
	}
	return true
}

func openToolbarDisplay() bool {
	if !loadGTK() {
		return false
	}
	name := os.Getenv("DISPLAY")
	if name == "" {
		return false
	}
	if toolbarDisplay == 0 {
		toolbarDisplay = gdkDisplayOpen(name)
	}
	if toolbarDisplay == 0 {
		return false
	}
	return true
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
