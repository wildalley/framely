//go:build linux

package capture

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// The XDG desktop portal is spoken through GIO, which GTK, and so every
// MyGo app on Linux, already loads: no D-Bus library of our own.
var (
	gioOnce sync.Once
	gioErr  error

	gBusGetSync                    func(busType int32, cancellable, err uintptr) uintptr
	gDBusConnectionCallSync        func(conn uintptr, name, path, iface, method *byte, params, replyType uintptr, flags int32, timeout int32, cancellable uintptr, err *unsafe.Pointer) uintptr
	gDBusConnectionSignalSubscribe func(conn uintptr, sender, iface, member, path, arg0 *byte, flags int32, callback, data, free uintptr) uint32
	gDBusConnectionSignalUnsub     func(conn uintptr, id uint32)
	gDBusConnectionGetUniqueName   func(conn uintptr) *byte
	gVariantParse                  func(typ uintptr, text *byte, limit, endptr, err uintptr) uintptr
	gVariantGetChildValue          func(v uintptr, i uint64) uintptr
	gVariantGetUint32              func(v uintptr) uint32
	gVariantGetString              func(v uintptr, length uintptr) *byte
	gVariantGetStrv                func(v uintptr, length *uint64) unsafe.Pointer
	gVariantLookupValue            func(dict uintptr, key *byte, typ uintptr) uintptr
	gVariantUnref                  func(v uintptr)
	gVariantRefSink                func(v uintptr) uintptr
	gFree                          func(p unsafe.Pointer)
	gErrorFree                     func(err unsafe.Pointer)

	signalCallback uintptr
	conn           uintptr
	mu             sync.Mutex
	waiting        = map[string]chan response{}
	tokens         int
	registerOnce   sync.Once
)

type response struct {
	code uint32
	uri  string
}

func loadGIO() error {
	gioOnce.Do(func() {
		lib, err := purego.Dlopen("libgio-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			gioErr = errors.New("截图门户不可用: 找不到 GIO")
			return
		}
		glib, err := purego.Dlopen("libglib-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			gioErr = errors.New("截图门户不可用: 找不到 GLib")
			return
		}
		purego.RegisterLibFunc(&gBusGetSync, lib, "g_bus_get_sync")
		purego.RegisterLibFunc(&gDBusConnectionCallSync, lib, "g_dbus_connection_call_sync")
		purego.RegisterLibFunc(&gDBusConnectionSignalSubscribe, lib, "g_dbus_connection_signal_subscribe")
		purego.RegisterLibFunc(&gDBusConnectionSignalUnsub, lib, "g_dbus_connection_signal_unsubscribe")
		purego.RegisterLibFunc(&gDBusConnectionGetUniqueName, lib, "g_dbus_connection_get_unique_name")
		purego.RegisterLibFunc(&gVariantParse, glib, "g_variant_parse")
		purego.RegisterLibFunc(&gVariantGetChildValue, glib, "g_variant_get_child_value")
		purego.RegisterLibFunc(&gVariantGetUint32, glib, "g_variant_get_uint32")
		purego.RegisterLibFunc(&gVariantGetString, glib, "g_variant_get_string")
		purego.RegisterLibFunc(&gVariantGetStrv, glib, "g_variant_get_strv")
		purego.RegisterLibFunc(&gVariantLookupValue, glib, "g_variant_lookup_value")
		purego.RegisterLibFunc(&gVariantUnref, glib, "g_variant_unref")
		purego.RegisterLibFunc(&gVariantRefSink, glib, "g_variant_ref_sink")
		purego.RegisterLibFunc(&gFree, glib, "g_free")
		purego.RegisterLibFunc(&gErrorFree, glib, "g_error_free")
		conn = gBusGetSync(2, 0, 0) // G_BUS_TYPE_SESSION
		if conn == 0 {
			gioErr = errors.New("截图门户不可用: 无法连接会话总线")
			return
		}
		signalCallback = purego.NewCallback(onResponse)
	})
	return gioErr
}

func cstr(s string) *byte {
	b := append([]byte(s), 0)
	return &b[0]
}

func gostr(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}

// parse builds a GVariant from its text form; the caller passes it to a
// call, which takes the floating reference.
func parse(text string) uintptr {
	return gVariantParse(0, cstr(text), 0, 0, 0)
}

// gvariantString quotes s for the GVariant text format.
func gvariantString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

func call(dest, path, iface, method, params, replyType string) (uintptr, error) {
	var gerr unsafe.Pointer
	var p uintptr
	if params != "" {
		p = parse(params)
		if p == 0 {
			return 0, errors.New("门户参数无效")
		}
	}
	var rt uintptr
	if replyType != "" {
		rt = parseType(replyType)
	}
	res := gDBusConnectionCallSync(conn, cstr(dest), cstr(path), cstr(iface), cstr(method), p, rt, 0, 30000, 0, &gerr)
	if res == 0 {
		msg := "门户调用失败"
		if gerr != nil {
			// GError { GQuark domain; gint code; gchar *message; }
			msg += ": " + gostr(*(**byte)(unsafe.Add(gerr, 8)))
			gErrorFree(gerr)
		}
		return 0, errors.New(msg)
	}
	return res, nil
}

var (
	typeOnce        sync.Once
	gVariantTypeNew func(s *byte) uintptr
)

func parseType(s string) uintptr {
	typeOnce.Do(func() {
		glib, _ := purego.Dlopen("libglib-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		purego.RegisterLibFunc(&gVariantTypeNew, glib, "g_variant_type_new")
	})
	return gVariantTypeNew(cstr(s))
}

// register tells the portal ScreenPin's app ID, which it knows only for
// sandboxed apps and apps the desktop launched; the permission store and
// the global shortcuts are keyed on it. It must come before any other
// portal call on the connection, and fails harmlessly when MyGo already
// registered the same ID on it.
func register() {
	registerOnce.Do(func() {
		res, err := call("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop",
			"org.freedesktop.host.portal.Registry", "Register", "("+gvariantString(AppID)+", @a{sv} {})", "")
		if err == nil {
			gVariantUnref(res)
		}
	})
}

func onResponse(_, _ uintptr, path *byte, _, _ *byte, params, _ uintptr) {
	p := gostr(path)
	mu.Lock()
	ch, ok := waiting[p]
	delete(waiting, p)
	mu.Unlock()
	if !ok {
		return
	}
	code := gVariantGetUint32(gVariantGetChildValue(params, 0))
	results := gVariantGetChildValue(params, 1)
	var uri string
	if v := gVariantLookupValue(results, cstr("uri"), parseType("s")); v != 0 {
		uri = gostr(gVariantGetString(v, 0))
		gVariantUnref(v)
	}
	gVariantUnref(results)
	ch <- response{code, uri}
}

// Screenshot asks the portal for a screenshot of the whole desktop, or,
// when interactive, of what the user picks in the desktop's own selector,
// and returns its PNG. It blocks: call it off the main thread, which must
// keep running for the portal's answer to arrive.
func Screenshot(interactive bool) ([]byte, error) {
	if err := loadGIO(); err != nil {
		return nil, err
	}
	register()
	mu.Lock()
	tokens++
	token := "screenpin" + strconv.Itoa(os.Getpid()) + "_" + strconv.Itoa(tokens)
	mu.Unlock()
	sender := strings.ReplaceAll(strings.TrimPrefix(gostr(gDBusConnectionGetUniqueName(conn)), ":"), ".", "_")
	path := "/org/freedesktop/portal/desktop/request/" + sender + "/" + token
	ch := make(chan response, 1)
	mu.Lock()
	waiting[path] = ch
	mu.Unlock()
	// Subscribe to every Response and pick ours by path: portals before
	// 0.9 choose the request's path themselves.
	sub := gDBusConnectionSignalSubscribe(conn, cstr("org.freedesktop.portal.Desktop"),
		cstr("org.freedesktop.portal.Request"), cstr("Response"), nil, nil, 0, signalCallback, 0, 0)
	defer gDBusConnectionSignalUnsub(conn, sub)
	mode := "false"
	if interactive {
		mode = "true"
	}
	res, err := call("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop",
		"org.freedesktop.portal.Screenshot", "Screenshot",
		"('', {'handle_token': <"+gvariantString(token)+">, 'interactive': <"+mode+">, 'modal': <"+mode+">})", "(o)")
	if err != nil {
		mu.Lock()
		delete(waiting, path)
		mu.Unlock()
		return nil, errors.New("截图门户不可用: " + err.Error())
	}
	handle := gostr(gVariantGetString(gVariantGetChildValue(res, 0), 0))
	gVariantUnref(res)
	if handle != path {
		mu.Lock()
		delete(waiting, path)
		waiting[handle] = ch
		mu.Unlock()
		path = handle
	}
	var r response
	select {
	case r = <-ch:
	case <-time.After(5 * time.Minute):
		mu.Lock()
		delete(waiting, path)
		mu.Unlock()
		return nil, errors.New("截图门户没有响应")
	}
	switch r.code {
	case 0:
	case 1:
		return nil, ErrCancelled
	default:
		return nil, errors.New("截图失败: 门户拒绝了请求")
	}
	u, err := url.Parse(r.uri)
	if err != nil || u.Scheme != "file" {
		return nil, errors.New("截图门户返回的不是本地文件")
	}
	data, err := os.ReadFile(u.Path)
	// The portal leaves a file behind for the caller; do not leave captured
	// pixels on disk once they are imported.
	_ = os.Remove(u.Path)
	if err != nil {
		return nil, errors.New("无法读取截图文件: " + err.Error())
	}
	return data, nil
}

// ScreenshotAccess reads the user's recorded answer for silent screenshots
// from the portal's permission store.
func ScreenshotAccess() Access {
	if loadGIO() != nil {
		return AccessUnsupported
	}
	res, err := call("org.freedesktop.impl.portal.PermissionStore", "/org/freedesktop/impl/portal/PermissionStore",
		"org.freedesktop.impl.portal.PermissionStore", "Lookup", "('screenshot', 'screenshot')", "(a{sas}v)")
	if err != nil {
		return AccessUnknown // no table yet: never asked
	}
	defer gVariantUnref(res)
	perms := gVariantGetChildValue(res, 0)
	defer gVariantUnref(perms)
	v := gVariantLookupValue(perms, cstr(AppID), parseType("as"))
	if v == 0 {
		return AccessUnknown
	}
	defer gVariantUnref(v)
	var n uint64
	arr := gVariantGetStrv(v, &n)
	defer gFree(arr)
	for i := range n {
		if gostr(*(**byte)(unsafe.Add(arr, i*8))) == "yes" {
			return AccessGranted
		}
	}
	return AccessDenied
}
