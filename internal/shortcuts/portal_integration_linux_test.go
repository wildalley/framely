//go:build linux

package shortcuts

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/portal"
	"github.com/wildalley/framely/internal/testutil"
)

type pendingBind struct {
	handle    dbus.ObjectPath
	session   dbus.ObjectPath
	shortcuts []portalShortcut
	parent    string
}

type shortcutPortal struct {
	conn          *dbus.Conn
	registered    chan string
	binds         chan pendingBind
	sessionClosed atomic.Int32
	requestClosed atomic.Int32
	createCode    atomic.Uint32
	configured    chan dbus.ObjectPath
}

type portalCloseCounter struct{ count *atomic.Int32 }

func (r *portalCloseCounter) Close() *dbus.Error { r.count.Add(1); return nil }

func (p *shortcutPortal) Register(id string, _ map[string]dbus.Variant) *dbus.Error {
	p.registered <- id
	return nil
}

func (p *shortcutPortal) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	return dbus.MakeVariant(uint32(2)), nil
}
func (p *shortcutPortal) ConfigureShortcuts(session dbus.ObjectPath, parent string, options map[string]dbus.Variant) *dbus.Error {
	p.configured <- session
	return nil
}

func requestPath(sender dbus.Sender, options map[string]dbus.Variant) dbus.ObjectPath {
	owner := strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_")
	return dbus.ObjectPath(string(portal.ObjectPath) + "/request/" + owner + "/legacy_" + options["handle_token"].Value().(string))
}

func (p *shortcutPortal) CreateSession(sender dbus.Sender, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	handle := requestPath(sender, options)
	owner := strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_")
	session := string(portal.ObjectPath) + "/session/" + owner + "/" + options["session_handle_token"].Value().(string)
	// Return the spec's string variant, and emit before the method reply.
	if err := p.conn.Emit(handle, "org.freedesktop.portal.Request.Response", p.createCode.Load(),
		map[string]dbus.Variant{"session_handle": dbus.MakeVariant(session)}); err != nil {
		return "", dbus.MakeFailedError(err)
	}
	return handle, nil
}

func TestPortalBackendFailureIsNotMisreportedAsUserDenial(t *testing.T) {
	provider, service, _ := shortcutService(t)
	service.createCode.Store(2) // Like a portal advertising an unusable backend.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, err := provider.Prepare(ctx, "Super+Shift+S", func(string) { t.Error("failed binding activated") })
	if b != nil || !errors.Is(err, portal.ErrFailed) || !strings.Contains(err.Error(), "CreateSession") ||
		!strings.Contains(err.Error(), "桌面无法创建") || strings.Contains(err.Error(), "拒绝了请求") {
		t.Fatalf("backend failure lost its diagnostic: binding=%v, err=%v", b, err)
	}
	if len(service.binds) != 0 {
		t.Fatal("BindShortcuts was called without a working session")
	}
}

func TestPortalAuthorizationFailureClosesCandidateSession(t *testing.T) {
	provider, service, _ := shortcutService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	bind, done := prepareBinding(t, ctx, provider, service, func(string) { t.Error("failed authorization fired") })
	emit(t, service, bind.handle, "org.freedesktop.portal.Request.Response", uint32(2), map[string]dbus.Variant{})
	select {
	case result := <-done:
		if result.binding != nil || !errors.Is(result.err, portal.ErrFailed) || service.sessionClosed.Load() != 1 {
			t.Fatalf("failed authorization: binding=%v, err=%v, session closes=%d", result.binding, result.err, service.sessionClosed.Load())
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func (p *shortcutPortal) BindShortcuts(sender dbus.Sender, session dbus.ObjectPath, shortcuts []portalShortcut, parent string, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	owner := strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_")
	handle := dbus.ObjectPath(string(portal.ObjectPath) + "/request/" + owner + "/" + options["handle_token"].Value().(string))
	p.binds <- pendingBind{handle, session, shortcuts, parent}
	// The method reply is not authorization: the test supplies Response later.
	return handle, nil
}

func shortcutService(t *testing.T) (*portalProvider, *shortcutPortal, chan func()) {
	t.Helper()
	testutil.SessionBus(t)
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	service := &shortcutPortal{conn: conn, registered: make(chan string, 1), binds: make(chan pendingBind, 1), configured: make(chan dbus.ObjectPath, 1)}
	for _, iface := range []string{shortcutInterface, "org.freedesktop.host.portal.Registry", "org.freedesktop.DBus.Properties"} {
		if err := conn.Export(service, portal.ObjectPath, iface); err != nil {
			t.Fatal(err)
		}
	}
	for _, spec := range []struct {
		path  string
		iface string
		count *atomic.Int32
	}{
		{"session", "org.freedesktop.portal.Session", &service.sessionClosed},
		{"request", "org.freedesktop.portal.Request", &service.requestClosed},
	} {
		if err := conn.ExportSubtree(&portalCloseCounter{spec.count}, dbus.ObjectPath(string(portal.ObjectPath)+"/"+spec.path), spec.iface); err != nil {
			t.Fatal(err)
		}
	}
	if reply, err := conn.RequestName(portal.Destination, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("owning fake portal name: reply=%v, err=%v", reply, err)
	}
	queue := make(chan func(), 8)
	provider := &portalProvider{dispatch: func(fn func()) { queue <- fn }}
	t.Cleanup(provider.Close)
	return provider, service, queue
}

func TestPortalUsesOneSessionAndSystemConfigurationForAllActions(t *testing.T) {
	p, service, queue := shortcutService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	captures, scrolls, records, updates := 0, 0, 0, 0
	m := &Manager{provider: p, capture: func(string) { captures++ }, scroll: func(string) { scrolls++ }, record: func(string) { records++ }, changed: func() { updates++ }, save: func(config.Config) error { return nil }}
	defer m.Close()
	done := make(chan error, 1)
	go func() { done <- m.Start(ctx, config.Default()) }()
	var bind pendingBind
	select {
	case bind = <-service.binds:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if len(bind.shortcuts) != 3 || bind.shortcuts[2].ID != recordID || bind.shortcuts[0].ID != captureID || bind.shortcuts[1].ID != scrollID || bind.shortcuts[1].Properties["preferred_trigger"].Value() != "LOGO+SHIFT+l" {
		t.Fatalf("actions: %+v", bind.shortcuts)
	}
	if _, set := bind.shortcuts[2].Properties["preferred_trigger"]; set {
		t.Fatal("claimed an unconfigured recording shortcut")
	}
	assigned := []portalShortcut{{ID: captureID, Properties: map[string]dbus.Variant{"trigger_description": dbus.MakeVariant("Press <Shift><Super>a")}}}
	emit(t, service, bind.handle, "org.freedesktop.portal.Request.Response", uint32(0), map[string]dbus.Variant{"shortcuts": dbus.MakeVariant(assigned)})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !m.SystemManaged() || m.Description() != "Press <Shift><Super>a" || m.ScrollDescription() != "" {
		t.Fatal("restored assignment was replaced by preference")
	}
	cfg := config.Default()
	cfg.CaptureShortcut = "Ctrl+Alt+S"
	if err := m.Apply(ctx, cfg); err == nil {
		t.Fatal("pretended that saving a preference changes the system binding")
	}
	if len(service.binds) != 0 || service.sessionClosed.Load() != 0 {
		t.Fatal("changing keys replaced the live session")
	}
	if err := m.Configure(ctx); err != nil {
		t.Fatal(err)
	}
	if session := <-service.configured; session != bind.session {
		t.Fatal("configured another session")
	}
	assigned = []portalShortcut{
		{ID: captureID, Properties: map[string]dbus.Variant{"trigger_description": dbus.MakeVariant("Super+Shift+S")}},
		{ID: scrollID, Properties: map[string]dbus.Variant{"trigger_description": dbus.MakeVariant("Super+Shift+L")}},
	}
	assigned = append(assigned, portalShortcut{ID: recordID, Properties: map[string]dbus.Variant{"trigger_description": dbus.MakeVariant("Super+Shift+R")}})
	emit(t, service, portal.ObjectPath, shortcutInterface+".ShortcutsChanged", bind.session, assigned)
	select {
	case fn := <-queue:
		fn()
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if updates != 1 || m.Description() != "Super+Shift+S" || m.ScrollDescription() != "Super+Shift+L" || m.RecordDescription() != "Super+Shift+R" {
		t.Fatal("system assignment change was not reflected")
	}
	for _, id := range []string{captureID, scrollID, recordID} {
		emit(t, service, portal.ObjectPath, shortcutInterface+".Activated", bind.session, id, uint64(1), map[string]dbus.Variant{})
		select {
		case fn := <-queue:
			fn()
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if captures != 1 || scrolls != 1 || records != 1 {
		t.Fatalf("routing capture=%d scroll=%d", captures, scrolls)
	}
	cfg = config.Default()
	cfg.HideAfterCopy = true
	if err := m.Apply(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if len(service.binds) != 0 {
		t.Fatal("ordinary save reregistered shortcuts")
	}
}

type preparedBinding struct {
	binding Binding
	err     error
}

func prepareBinding(t *testing.T, ctx context.Context, provider *portalProvider, service *shortcutPortal, capture func(string)) (pendingBind, <-chan preparedBinding) {
	t.Helper()
	done := make(chan preparedBinding, 1)
	go func() {
		binding, err := provider.Prepare(ctx, "Super+Shift+S", capture)
		done <- preparedBinding{binding, err}
	}()
	select {
	case id := <-service.registered:
		if id != portal.AppID {
			t.Fatalf("registered app ID %q", id)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case bind := <-service.binds:
		if len(bind.shortcuts) != 1 || bind.shortcuts[0].ID != captureID || bind.parent != "" ||
			bind.shortcuts[0].Properties["preferred_trigger"].Value() != "LOGO+SHIFT+s" {
			t.Fatalf("unexpected wire binding: %#v", bind)
		}
		select {
		case result := <-done:
			t.Fatalf("Prepare completed before authorization: %v", result.err)
		default:
		}
		return bind, done
	case result := <-done:
		t.Fatalf("Prepare failed before BindShortcuts: %v", result.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return pendingBind{}, done
}

func emit(t *testing.T, service *shortcutPortal, path dbus.ObjectPath, name string, body ...any) {
	t.Helper()
	if err := service.conn.Emit(path, name, body...); err != nil {
		t.Fatal(err)
	}
}

func TestPortalBindingWaitsForAuthorizationAndFiltersActivations(t *testing.T) {
	provider, service, queue := shortcutService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	captures := make(chan string, 4)
	bind, done := prepareBinding(t, ctx, provider, service, func(token string) { captures <- token })
	assigned := []portalShortcut{{ID: captureID, Properties: map[string]dbus.Variant{
		"trigger_description": dbus.MakeVariant("Super+Alt+S"),
	}}}
	emit(t, service, bind.handle, "org.freedesktop.portal.Request.Response", uint32(0), map[string]dbus.Variant{"shortcuts": dbus.MakeVariant(assigned)})
	var b Binding
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		b = result.binding
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	t.Cleanup(b.Close)
	if b.Description() != "Super+Alt+S" {
		t.Fatalf("the actual assigned key was lost: %q", b.Description())
	}
	b.Activate()
	options := map[string]dbus.Variant{"activation_token": dbus.MakeVariant("window-token")}
	emit(t, service, portal.ObjectPath, shortcutInterface+".Activated", "malformed")
	emit(t, service, portal.ObjectPath, shortcutInterface+".Activated", dbus.ObjectPath(string(portal.ObjectPath)+"/session/other/old"), captureID, uint64(0), options)
	emit(t, service, portal.ObjectPath, shortcutInterface+".Activated", bind.session, "another-action", uint64(0), options)
	emit(t, service, portal.ObjectPath, shortcutInterface+".Activated", bind.session, captureID, uint64(0), options)
	select {
	case fn := <-queue:
		fn()
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case token := <-captures:
		if token != "window-token" {
			t.Fatalf("activation token %q", token)
		}
	default:
		t.Fatal("valid activation did not reach capture")
	}
	// A callback already queued when the binding closes must also be inert.
	emit(t, service, portal.ObjectPath, shortcutInterface+".Activated", bind.session, captureID, uint64(0), options)
	select {
	case fn := <-queue:
		b.Close()
		b.Close()
		fn()
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if len(captures) != 0 || service.sessionClosed.Load() != 1 {
		t.Fatalf("stale activation or unclosed session: captures=%d, closes=%d", len(captures), service.sessionClosed.Load())
	}
}

func TestPortalBindingCancellationClosesRequestAndSession(t *testing.T) {
	provider, service, _ := shortcutService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, done := prepareBinding(t, ctx, provider, service, func(string) { t.Error("cancelled binding fired") })
	cancel()
	select {
	case result := <-done:
		if result.binding != nil || !errors.Is(result.err, context.Canceled) ||
			service.sessionClosed.Load() != 1 || service.requestClosed.Load() != 1 {
			t.Fatalf("binding=%v, err=%v, session closes=%d, request closes=%d", result.binding, result.err, service.sessionClosed.Load(), service.requestClosed.Load())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelling the authorization did not return")
	}
}
