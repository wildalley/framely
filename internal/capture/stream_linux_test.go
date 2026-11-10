//go:build linux

package capture

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
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
		_ = binary.Write(os.Stdout, binary.LittleEndian, [9]uint32{0x32465053, 2, 1, 1, 0, 0, 2, 1, 0})
		_, _ = os.Stdout.Write([]byte{3, 2, 1, 255, 6, 5, 4, 255})
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			var id, x, y, w, h, sw, sh uint32
			if n, err := fmt.Sscanf(scanner.Text(), "crop %d %d %d %d %d %d %d", &id, &x, &y, &w, &h, &sw, &sh); err != nil || n != 7 {
				os.Exit(3)
			}
			if x != 1 || y != 0 || w != 1 || h != 1 || sw != 2 || sh != 1 {
				os.Exit(4)
			}
			_ = binary.Write(os.Stdout, binary.LittleEndian, [4]uint32{0x31435053, 0, 0, id})
			_ = binary.Write(os.Stdout, binary.LittleEndian, [9]uint32{0x32465053, w, h, 1, x, y, sw, sh, id})
			_, _ = os.Stdout.Write([]byte{6, 5, 4, 255})
		}
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
	omitStreams                      bool
	version                          uint32
	sourcePersist, devicePersist     uint32
	sourceRestore, deviceRestore     string
	newRestore                       string
	starts                           []captureStart
	startCount, remoteOpened         int
	sourceRestores, deviceRestores   []string
}

type captureStart struct {
	bounds     image.Rectangle
	sourceType uint32
	restore    string
	code       uint32
}

func (p *capturePortal) Register(string, map[string]dbus.Variant) *dbus.Error { return nil }
func (p *capturePortal) Get(_ string, name string) (dbus.Variant, *dbus.Error) {
	if name == "version" {
		return dbus.MakeVariant(p.version), nil
	}
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
	p.sourcePersist, _ = options["persist_mode"].Value().(uint32)
	p.sourceRestore, _ = options["restore_token"].Value().(string)
	p.sourceRestores = append(p.sourceRestores, p.sourceRestore)
	p.mu.Unlock()
	return p.respond(sender, options, 0, nil)
}
func (p *capturePortal) SelectDevices(sender dbus.Sender, _ dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p.mu.Lock()
	p.selectedDevices = options["types"].Value().(uint32)
	p.devicePersist, _ = options["persist_mode"].Value().(uint32)
	p.deviceRestore, _ = options["restore_token"].Value().(string)
	p.deviceRestores = append(p.deviceRestores, p.deviceRestore)
	p.mu.Unlock()
	return p.respond(sender, options, 0, nil)
}
func (p *capturePortal) Start(sender dbus.Sender, _ dbus.ObjectPath, _ string, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p.mu.Lock()
	code, devices, omitStreams, restore := p.startCode, p.devices, p.omitStreams, p.newRestore
	properties := map[string]dbus.Variant{"logical_size": dbus.MakeVariant(struct{ W, H int32 }{200, 100})}
	if p.startCount < len(p.starts) {
		start := p.starts[p.startCount]
		code, restore = start.code, start.restore
		properties["source_type"] = dbus.MakeVariant(start.sourceType)
		if !start.bounds.Empty() {
			properties["position"] = dbus.MakeVariant(struct{ X, Y int32 }{int32(start.bounds.Min.X), int32(start.bounds.Min.Y)})
			properties["size"] = dbus.MakeVariant(struct{ W, H int32 }{int32(start.bounds.Dx()), int32(start.bounds.Dy())})
		}
	}
	p.startCount++
	p.mu.Unlock()
	result := map[string]dbus.Variant{
		"devices": dbus.MakeVariant(devices),
		"streams": dbus.MakeVariant([]portalStream{{Node: 42, Properties: properties}}),
	}
	if omitStreams {
		delete(result, "streams")
	}
	if restore != "" {
		result["restore_token"] = dbus.MakeVariant(restore)
	}
	return p.respond(sender, options, code, result)
}
func (p *capturePortal) OpenPipeWireRemote(dbus.ObjectPath, map[string]dbus.Variant) (dbus.UnixFD, *dbus.Error) {
	p.mu.Lock()
	p.remoteOpened++
	p.mu.Unlock()
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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
	p := &capturePortal{conn: conn, fd: read, devices: 2, version: 4}
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
			if err := SetStreamRegion(ctx, s, image.Rect(1, 0, 2, 1), image.Pt(2, 1)); err != nil {
				t.Fatal(err)
			}
			cropped, err := s.Next(ctx)
			if err != nil || cropped.Rect != image.Rect(1, 0, 2, 1) || cropped.RGBAAt(1, 0).R != 4 {
				t.Fatalf("crop: %v, %v", cropped, err)
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

func TestPortalDisplayBoundsRequireWholeDisplayAndValidGeometry(t *testing.T) {
	properties := map[string]dbus.Variant{
		"source_type": dbus.MakeVariant(uint32(1)),
		"position":    dbus.MakeVariant(struct{ X, Y int32 }{-1280, 0}),
		"size":        dbus.MakeVariant(struct{ W, H int32 }{1280, 720}),
	}
	if got := portalDisplayBounds(properties); got != image.Rect(-1280, 0, 0, 720) {
		t.Fatal(got)
	}
	properties["source_type"] = dbus.MakeVariant(uint32(2))
	if !portalDisplayBounds(properties).Empty() {
		t.Fatal("window stream mapped onto a whole display")
	}
	properties["source_type"] = dbus.MakeVariant(uint32(1))
	delete(properties, "position")
	if !portalDisplayBounds(properties).Empty() {
		t.Fatal("guessed absent display coordinates")
	}
}

func TestPortalMissingStreamReturnsErrorAndReleasesSession(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "automatic"}[automatic], func(t *testing.T) {
			p := captureService(t)
			p.mu.Lock()
			p.omitStreams = true
			p.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			source, err := OpenStream(ctx, StreamOptions{Automatic: automatic})
			if source != nil || err == nil {
				t.Fatal("missing stream was accepted")
			}
			if automatic && !errors.Is(err, ErrAutomaticUnavailable) {
				t.Fatal("automatic capture cannot fall back:", err)
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.closed != 1 {
				t.Fatal("missing stream leaked session", p.closed)
			}
		})
	}
}

func TestPortalSourcePersistenceUsesCorrectInterfaceAndRotatesToken(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "screen", true: "pointer"}[automatic], func(t *testing.T) {
			p := captureService(t)
			iface := screenCast
			if automatic {
				iface = remoteDesktop
			}
			if err := saveRestoreToken(iface, "previous-grant"); err != nil {
				t.Fatal(err)
			}
			p.mu.Lock()
			p.newRestore = "renewed-grant"
			p.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			source, err := OpenStream(ctx, StreamOptions{Automatic: automatic})
			if err != nil {
				t.Fatal(err)
			}
			source.Close()
			p.mu.Lock()
			if automatic {
				if p.devicePersist != 2 || p.deviceRestore != "previous-grant" || p.sourcePersist != 0 || p.sourceRestore != "" {
					t.Fatal("pointer grant sent to wrong interface")
				}
			} else if p.sourcePersist != 2 || p.sourceRestore != "previous-grant" || p.devicePersist != 0 {
				t.Fatal("screen grant sent to wrong interface")
			}
			p.mu.Unlock()
			if token := takeRestoreToken(iface); token != "renewed-grant" {
				t.Fatalf("token was not rotated: %q", token)
			}
			if takeRestoreToken(iface) != "" {
				t.Fatal("single-use token could be reused")
			}
			path, _ := restorePath()
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("source grants not stored privately")
			}
		})
	}
}

func TestDisplayRestoreTokensAreSeparateAndSingleUse(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	left, right := image.Rect(-1280, 0, 0, 720), image.Rect(0, 0, 1920, 1080)
	for _, grant := range []struct {
		iface, token string
		bounds       image.Rectangle
	}{{screenCast, "left-screen", left}, {screenCast, "right-screen", right}, {remoteDesktop, "left-pointer", left}} {
		if err := saveDisplayRestoreToken(grant.iface, grant.token, grant.bounds); err != nil {
			t.Fatal(err)
		}
	}
	if token, _ := takeDisplayRestoreToken(screenCast, image.Rect(0, -1080, 1920, 0)); token != "" {
		t.Fatal("new monitor borrowed another display's grant")
	}
	if token, _ := takeDisplayRestoreToken(screenCast, left); token != "left-screen" {
		t.Fatal("did not select the exact display grant")
	}
	if token, _ := takeDisplayRestoreToken(screenCast, left); token != "" {
		t.Fatal("consumed display grant could be reused")
	}
	if token, _ := takeDisplayRestoreToken(remoteDesktop, left); token != "left-pointer" {
		t.Fatal("screen grant consumed pointer grant")
	}
	// A rotated grant cannot remain available under two display keys.
	if err := saveDisplayRestoreToken(screenCast, "right-screen", left); err != nil {
		t.Fatal(err)
	}
	if token, _ := takeDisplayRestoreToken(screenCast, right); token != "" {
		t.Fatal("same grant remained under the old display key")
	}
	if token, _ := takeDisplayRestoreToken(screenCast, left); token != "right-screen" {
		t.Fatal("rotated grant not saved at the new geometry")
	}
}

func TestPortalRestoresOnlyRequestedDisplay(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		for _, granted := range []bool{false, true} {
			t.Run(fmt.Sprintf("automatic_%t/granted_%t", automatic, granted), func(t *testing.T) {
				p := captureService(t)
				left, right := image.Rect(-1280, 0, 0, 720), image.Rect(0, 0, 1920, 1080)
				iface := screenCast
				if automatic {
					iface = remoteDesktop
				}
				if err := saveDisplayRestoreToken(iface, "left-grant", left); err != nil {
					t.Fatal(err)
				}
				wantRestore := ""
				if granted {
					wantRestore = "right-grant"
					if err := saveDisplayRestoreToken(iface, wantRestore, right); err != nil {
						t.Fatal(err)
					}
				}
				p.mu.Lock()
				p.starts = []captureStart{{bounds: right, sourceType: 1, restore: "right-renewed"}}
				p.mu.Unlock()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				source, err := OpenStream(ctx, StreamOptions{Automatic: automatic, DisplayBounds: right})
				if err != nil {
					t.Fatal(err)
				}
				if bounds, ok := StreamDisplayBounds(source); !ok || bounds != right {
					t.Fatal("stream lost display geometry", bounds)
				}
				source.Close()
				p.mu.Lock()
				actualRestore := p.sourceRestore
				if automatic {
					actualRestore = p.deviceRestore
				}
				if actualRestore != wantRestore || p.created != 1 || p.closed != 1 || p.remoteOpened != 1 {
					t.Errorf("unexpected restored source or session count: %+v", p)
				}
				p.mu.Unlock()
				if token, _ := takeDisplayRestoreToken(iface, left); token != "left-grant" {
					t.Fatal("other monitor lost its grant")
				}
				if token, _ := takeDisplayRestoreToken(iface, right); token != "right-renewed" {
					t.Fatal("requested monitor token not rotated")
				}
			})
		}
	}
}

func TestPortalWrongRestoredDisplayRetriesChooserAndPreservesGrant(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		for _, cancelRetry := range []bool{false, true} {
			t.Run(fmt.Sprintf("automatic_%t/cancel_%t", automatic, cancelRetry), func(t *testing.T) {
				p := captureService(t)
				left, right := image.Rect(-1280, 0, 0, 720), image.Rect(0, 0, 1920, 1080)
				iface := screenCast
				if automatic {
					iface = remoteDesktop
				}
				if err := saveRestoreToken(iface, "legacy-grant"); err != nil {
					t.Fatal(err)
				}
				code := uint32(0)
				if cancelRetry {
					code = 1
				}
				p.mu.Lock()
				p.starts = []captureStart{
					{bounds: left, sourceType: 1, restore: "left-renewed"},
					{bounds: right, sourceType: 1, restore: "right-renewed", code: code},
				}
				p.mu.Unlock()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				source, err := OpenStream(ctx, StreamOptions{Automatic: automatic, DisplayBounds: right})
				if cancelRetry {
					if source != nil || !errors.Is(err, portal.ErrCancelled) {
						t.Fatal("chooser cancellation not returned", err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if _, err := source.Next(ctx); err != nil {
						t.Fatal("wrong session started a helper and consumed its FD", err)
					}
					source.Close()
				}
				p.mu.Lock()
				restores := p.sourceRestores
				if automatic {
					restores = p.deviceRestores
				}
				wantRemote := 1
				if cancelRetry {
					wantRemote = 0
				}
				if len(restores) != 2 || restores[0] != "legacy-grant" || restores[1] != "" || p.created != 2 || p.closed != 2 || p.remoteOpened != wantRemote {
					t.Errorf("retry restored wrong grant or leaked session: %+v", p)
				}
				p.mu.Unlock()
				if token, _ := takeDisplayRestoreToken(iface, left); token != "left-renewed" {
					t.Fatal("legacy grant was not migrated to its actual monitor")
				}
				if token := takeRestoreToken(iface); token != "" {
					t.Fatal("legacy grant remained available")
				}
				wantToken := "right-renewed"
				if cancelRetry {
					wantToken = ""
				}
				if token, _ := takeDisplayRestoreToken(iface, right); token != wantToken {
					t.Fatal("unexpected grant after chooser")
				}
			})
		}
	}
}

func TestPortalFreshExplicitSourceSelectionIsRespected(t *testing.T) {
	for _, sourceType := range []uint32{1, 2} {
		t.Run(fmt.Sprintf("source_%d", sourceType), func(t *testing.T) {
			p := captureService(t)
			left, right := image.Rect(-1280, 0, 0, 720), image.Rect(0, 0, 1920, 1080)
			p.mu.Lock()
			p.starts = []captureStart{{bounds: left, sourceType: sourceType, restore: "explicit-grant"}}
			p.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			source, err := OpenStream(ctx, StreamOptions{DisplayBounds: right})
			if err != nil {
				t.Fatal("explicit selection rejected", err)
			}
			source.Close()
			p.mu.Lock()
			if p.created != 1 || p.closed != 1 || p.remoteOpened != 1 {
				t.Errorf("explicit selection opened another chooser: %+v", p)
			}
			p.mu.Unlock()
			bounds := left
			if sourceType == 2 {
				bounds = right // windows have no whole-display geometry
			}
			if token, _ := takeDisplayRestoreToken(screenCast, bounds); token != "explicit-grant" {
				t.Fatal("explicit source grant not retained")
			}
		})
	}
}
