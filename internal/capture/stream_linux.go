//go:build linux

package capture

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/wildalley/screenpin/internal/components"
	"github.com/wildalley/screenpin/internal/portal"
)

const screenCast = "org.freedesktop.portal.ScreenCast"
const remoteDesktop = "org.freedesktop.portal.RemoteDesktop"

var streamTokens atomic.Uint64

type portalStream struct {
	Node       uint32
	Properties map[string]dbus.Variant
}

func StreamMonitors(context.Context) ([]Monitor, error) { return nil, nil }

func AutomaticScrollAvailable(ctx context.Context) bool {
	c, err := portal.New()
	if err != nil {
		return false
	}
	defer c.Close()
	return pointerAvailable(ctx, c)
}

func pointerAvailable(ctx context.Context, c *portal.Client) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var value dbus.Variant
	if c.Call(ctx, portal.Destination, portal.ObjectPath, "org.freedesktop.DBus.Properties.Get", remoteDesktop, "AvailableDeviceTypes").Store(&value) != nil {
		return false
	}
	types, ok := value.Value().(uint32)
	return ok && types&2 != 0
}

func OpenStream(parent context.Context, options StreamOptions) (Stream, error) {
	helper, err := components.Find("screenpin-capture")
	if err != nil {
		return nil, err
	}
	c, err := portal.New()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	var session dbus.ObjectPath
	cleanup := func() {
		if session.IsValid() {
			closeCtx, done := context.WithTimeout(context.Background(), 2*time.Second)
			defer done()
			_ = c.Call(closeCtx, portal.Destination, session, "org.freedesktop.portal.Session.Close").Err
		}
		_ = c.Close()
	}
	fail := func(err error) (Stream, error) { cancel(); cleanup(); return nil, err }
	iface := screenCast
	if options.Automatic {
		if !pointerAvailable(ctx, c) {
			return fail(ErrAutomaticUnavailable)
		}
		iface = remoteDesktop
	}
	requestCtx, requestDone := context.WithTimeout(ctx, 2*time.Minute)
	defer requestDone()
	result, err := c.Request(requestCtx, iface, "CreateSession", nil, map[string]dbus.Variant{"session_handle_token": dbus.MakeVariant(fmt.Sprintf("screenpin_capture_%d_%d", os.Getpid(), streamTokens.Add(1)))})
	if err != nil {
		return fail(err)
	}
	switch v := result["session_handle"].Value().(type) {
	case string:
		session = dbus.ObjectPath(v)
	case dbus.ObjectPath:
		session = v
	}
	if !session.IsValid() || !strings.HasPrefix(string(session), string(portal.ObjectPath)+"/session/") {
		return fail(errors.New("屏幕共享门户返回了无效会话"))
	}
	if options.Automatic {
		if _, err = c.Request(requestCtx, remoteDesktop, "SelectDevices", []any{session}, map[string]dbus.Variant{"types": dbus.MakeVariant(uint32(2))}); err != nil {
			return fail(err)
		}
	}
	if _, err = c.Request(requestCtx, screenCast, "SelectSources", []any{session}, map[string]dbus.Variant{
		"types": dbus.MakeVariant(uint32(3)), "multiple": dbus.MakeVariant(false), "cursor_mode": dbus.MakeVariant(uint32(1)),
	}); err != nil {
		return fail(err)
	}
	result, err = c.Request(requestCtx, iface, "Start", []any{session, ""}, nil)
	if err != nil {
		return fail(err)
	}
	if options.Automatic {
		types, _ := result["devices"].Value().(uint32)
		if types&2 == 0 {
			return fail(ErrAutomaticUnavailable)
		}
	}
	var streams []portalStream
	if dbus.Store([]any{result["streams"].Value()}, &streams) != nil || len(streams) != 1 {
		return fail(errors.New("请选择一个窗口或显示器"))
	}
	var fd dbus.UnixFD
	if err = c.Call(requestCtx, portal.Destination, portal.ObjectPath, screenCast+".OpenPipeWireRemote", session, map[string]dbus.Variant{}).Store(&fd); err != nil {
		return fail(err)
	}
	file := os.NewFile(uintptr(fd), "screenpin-pipewire")
	if file == nil {
		return fail(errors.New("PipeWire 连接无效"))
	}
	cmd := exec.CommandContext(ctx, helper, "--fd", "3", "--node", strconv.FormatUint(uint64(streams[0].Node), 10))
	cmd.ExtraFiles = []*os.File{file}
	s, err := startStream(ctx, cmd, cancel, cleanup)
	_ = file.Close()
	if err != nil {
		return nil, err
	}
	if options.Automatic {
		logicalW, logicalH := streamSize(streams[0].Properties["logical_size"])
		if logicalW == 0 {
			logicalW, logicalH = streamSize(streams[0].Properties["size"])
		}
		s.scroll = func(callCtx context.Context, p image.Point) error {
			if err := callCtx.Err(); err != nil {
				return err
			}
			w, h := float64(s.width.Load()), float64(s.height.Load())
			if w <= 0 || h <= 0 {
				return errors.New("尚未获得屏幕尺寸")
			}
			lw, lh := logicalW, logicalH
			if lw == 0 || lh == 0 {
				lw, lh = w, h
			}
			if err := c.Call(callCtx, portal.Destination, portal.ObjectPath, remoteDesktop+".NotifyPointerMotionAbsolute", session, map[string]dbus.Variant{}, streams[0].Node, float64(p.X)*lw/w, float64(p.Y)*lh/h).Err; err != nil {
				return err
			}
			return c.Call(callCtx, portal.Destination, portal.ObjectPath, remoteDesktop+".NotifyPointerAxisDiscrete", session, map[string]dbus.Variant{}, uint32(0), int32(1)).Err
		}
	}
	return s, nil
}

func streamSize(v dbus.Variant) (float64, float64) {
	var pair struct{ W, H int32 }
	if dbus.Store([]any{v.Value()}, &pair) != nil || pair.W <= 0 || pair.H <= 0 {
		return 0, 0
	}
	return float64(pair.W), float64(pair.H)
}
