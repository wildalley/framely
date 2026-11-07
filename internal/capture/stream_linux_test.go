//go:build linux

package capture

import (
	"context"
	"encoding/binary"
	"errors"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/wildalley/screenpin/internal/portal"
	"github.com/wildalley/screenpin/internal/testutil"
)

func TestMain(m *testing.M) {
	if os.Getenv("SCREENPIN_CAPTURE_TEST_WORKER") == "1" {
		fd := os.NewFile(3, "inherited-pipewire")
		var token [2]byte
		if _, err := io.ReadFull(fd, token[:]); err != nil || string(token[:]) != "OK" {
			os.Exit(2)
		}
		_ = binary.Write(os.Stdout, binary.LittleEndian, [4]uint32{0x31465053, 2, 1, 1})
		_, _ = os.Stdout.Write([]byte{3, 2, 1, 255, 6, 5, 4, 255})
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type capturePortal struct {
	conn                             *dbus.Conn
	fd                               *os.File
	devices, startCode               uint32
	mu                               sync.Mutex
	closed, created                  int
	selectedSources, selectedDevices uint32
	moved                            [2]float64
	axis                             uint32
	steps                            int32
}

func (p *capturePortal) Register(string, map[string]dbus.Variant) *dbus.Error { return nil }
func (p *capturePortal) Get(string, string) (dbus.Variant, *dbus.Error) {
	return dbus.MakeVariant(uint32(2)), nil
}
func (p *capturePortal) respond(sender dbus.Sender, options map[string]dbus.Variant, code uint32, result map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	owner := strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_")
	handle := dbus.ObjectPath(string(portal.ObjectPath) + "/request/" + owner + "/" + options["handle_token"].Value().(string))
	if err := p.conn.Emit(handle, "org.freedesktop.portal.Request.Response", code, result); err != nil {
		return "", dbus.MakeFailedError(err)
	}
	return handle, nil
}
func (p *capturePortal) CreateSession(sender dbus.Sender, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p.mu.Lock()
	p.created++
	p.mu.Unlock()
	return p.respond(sender, options, 0, map[string]dbus.Variant{"session_handle": dbus.MakeVariant(string(portal.ObjectPath) + "/session/test/capture")})
}
func (p *capturePortal) SelectSources(sender dbus.Sender, _ dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p.mu.Lock()
	p.selectedSources = options["types"].Value().(uint32)
	p.mu.Unlock()
	return p.respond(sender, options, 0, nil)
}
func (p *capturePortal) SelectDevices(sender dbus.Sender, _ dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p.mu.Lock()
	p.selectedDevices = options["types"].Value().(uint32)
	p.mu.Unlock()
	return p.respond(sender, options, 0, nil)
}
func (p *capturePortal) Start(sender dbus.Sender, _ dbus.ObjectPath, _ string, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p.mu.Lock()
	code, devices := p.startCode, p.devices
	p.mu.Unlock()
	return p.respond(sender, options, code, map[string]dbus.Variant{
		"devices": dbus.MakeVariant(devices),
		"streams": dbus.MakeVariant([]portalStream{{Node: 42, Properties: map[string]dbus.Variant{"logical_size": dbus.MakeVariant(struct{ W, H int32 }{200, 100})}}}),
	})
}
func (p *capturePortal) OpenPipeWireRemote(dbus.ObjectPath, map[string]dbus.Variant) (dbus.UnixFD, *dbus.Error) {
	return dbus.UnixFD(p.fd.Fd()), nil
}
func (p *capturePortal) NotifyPointerMotionAbsolute(_ dbus.ObjectPath, _ map[string]dbus.Variant, _ uint32, x, y float64) *dbus.Error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.moved = [2]float64{x, y}
	return nil
}
func (p *capturePortal) NotifyPointerAxisDiscrete(_ dbus.ObjectPath, _ map[string]dbus.Variant, axis uint32, steps int32) *dbus.Error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.axis, p.steps = axis, steps
	return nil
}
func (p *capturePortal) Close() *dbus.Error { p.mu.Lock(); defer p.mu.Unlock(); p.closed++; return nil }

func captureService(t *testing.T) *capturePortal {
	t.Helper()
	testutil.SessionBus(t)
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = write.Write([]byte("OK"))
	_ = write.Close()
	t.Cleanup(func() { _ = read.Close() })
	p := &capturePortal{conn: conn, fd: read, devices: 2}
	for _, iface := range []string{screenCast, remoteDesktop, "org.freedesktop.host.portal.Registry", "org.freedesktop.DBus.Properties"} {
		if err := conn.Export(p, portal.ObjectPath, iface); err != nil {
			t.Fatal(err)
		}
	}
	if err := conn.Export(p, dbus.ObjectPath(string(portal.ObjectPath)+"/session/test/capture"), "org.freedesktop.portal.Session"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.RequestName(portal.Destination, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	executable, _ := os.Executable()
	dir := t.TempDir()
	if err := os.Symlink(executable, filepath.Join(dir, "screenpin-capture")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCREENPIN_COMPONENTS_DIR", dir)
	t.Setenv("SCREENPIN_CAPTURE_TEST_WORKER", "1")
	return p
}

func TestPortalStreamFDCoordinatesAndCleanup(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "automatic"}[automatic], func(t *testing.T) {
			p := captureService(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, err := OpenStream(ctx, StreamOptions{Automatic: automatic})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			frame, err := s.Next(ctx)
			if err != nil || frame.RGBAAt(0, 0).R != 1 {
				t.Fatalf("inherited FD/frame: %v %v", frame, err)
			}
			err = s.Scroll(ctx, image.Pt(1, 0))
			if automatic && err != nil || !automatic && !errors.Is(err, ErrAutomaticUnavailable) {
				t.Fatal(err)
			}
			s.Close()
			s.Close()
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.closed != 1 || p.selectedSources != 3 {
				t.Fatalf("session/source cleanup: %+v", p)
			}
			if automatic && (p.selectedDevices != 2 || p.moved != [2]float64{100, 0} || p.axis != 0 || p.steps != 1) {
				t.Fatalf("pointer mapping: %+v", p)
			}
		})
	}
}
func TestPortalStreamDeniedAndCancelledReleaseSession(t *testing.T) {
	for _, code := range []uint32{0, 1} {
		t.Run(map[uint32]string{0: "no_pointer_grant", 1: "cancelled"}[code], func(t *testing.T) {
			p := captureService(t)
			p.mu.Lock()
			p.devices = 0
			p.startCode = code
			p.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, err := OpenStream(ctx, StreamOptions{Automatic: true})
			want := ErrAutomaticUnavailable
			if code == 1 {
				want = portal.ErrCancelled
			}
			if s != nil || !errors.Is(err, want) {
				t.Fatalf("stream=%v error=%v", s, err)
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.closed != 1 {
				t.Fatalf("leaked candidate session: %d", p.closed)
			}
		})
	}
}
