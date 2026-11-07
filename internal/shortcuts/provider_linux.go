//go:build linux

package shortcuts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/godbus/dbus/v5"
	"github.com/wildalley/screenpin/internal/config"
	"github.com/wildalley/screenpin/internal/niribind"
	"github.com/wildalley/screenpin/internal/portal"
	"github.com/wildalley/screenpin/internal/shortcutkey"
)

const shortcutInterface = "org.freedesktop.portal.GlobalShortcuts"
const captureID = "capture"

func newPlatformProvider() Provider {
	if os.Getenv("XDG_SESSION_TYPE") == "wayland" || os.Getenv("WAYLAND_DISPLAY") != "" {
		return &waylandProvider{}
	}
	return nativeProvider{}
}

type waylandProvider struct{ portalProvider }
type niriBinding struct{ key string }

func (p *waylandProvider) Prepare(ctx context.Context, key string, capture func(string)) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if niribind.Matches(key) {
		if err := niribind.Verify(ctx); err != nil {
			return nil, err
		}
		return &niriBinding{key: key}, nil
	}
	return p.portalProvider.Prepare(ctx, key, capture)
}

// niri owns this persistent desktop binding; exiting ScreenPin leaves it able
// to launch the app again. No application listener is registered twice.
func (b *niriBinding) Activate()           {}
func (b *niriBinding) Close()              {}
func (b *niriBinding) Description() string { return config.DisplayShortcut(b.key) + "（niri）" }

type portalProvider struct {
	client   *portal.Client
	dispatch func(func())
	tokens   atomic.Uint64
}

type portalBinding struct {
	client      *portal.Client
	session     dbus.ObjectPath
	description string
	sub         *portal.Subscription
	cancel      context.CancelFunc
	dispatch    func(func())
	active      atomic.Bool
	once        sync.Once
}

type portalShortcut struct {
	ID         string
	Properties map[string]dbus.Variant
}

func (p *portalProvider) Prepare(ctx context.Context, key string, capture func(string)) (_ Binding, err error) {
	if p.client == nil {
		if p.client, err = portal.New(); err != nil {
			return nil, err
		}
	}
	parsed, err := shortcutkey.Parse(key, "linux")
	if err != nil {
		return nil, err
	}
	result, err := p.client.Request(ctx, shortcutInterface, "CreateSession", nil, map[string]dbus.Variant{
		"session_handle_token": dbus.MakeVariant(fmt.Sprintf("screenpin_%d_%d", os.Getpid(), p.tokens.Add(1))),
	})
	if err != nil {
		if errors.Is(err, portal.ErrFailed) {
			return nil, fmt.Errorf("桌面无法创建全局快捷键会话，请检查当前桌面的门户支持；可在桌面设置中绑定 screenpin --capture: %w", err)
		}
		return nil, err
	}
	var session dbus.ObjectPath
	switch value := result["session_handle"].Value().(type) {
	case dbus.ObjectPath:
		session = value
	case string:
		session = dbus.ObjectPath(value)
	}
	if !session.IsValid() || !strings.HasPrefix(string(session), string(portal.ObjectPath)+"/session/") {
		return nil, errors.New("快捷键门户没有返回有效的会话")
	}
	dispatch := p.dispatch
	if dispatch == nil {
		dispatch = mygo.RunOnMain
	}
	b := &portalBinding{client: p.client, session: session, dispatch: dispatch}
	defer func() {
		if err != nil {
			b.Close()
		}
	}()
	b.sub, err = p.client.Subscribe(dbus.WithMatchSender(portal.Destination),
		dbus.WithMatchInterface(shortcutInterface), dbus.WithMatchMember("Activated"), dbus.WithMatchObjectPath(portal.ObjectPath))
	if err != nil {
		return nil, err
	}
	list := []portalShortcut{{ID: captureID, Properties: map[string]dbus.Variant{
		"description":       dbus.MakeVariant("截取屏幕"),
		"preferred_trigger": dbus.MakeVariant(parsed.XDGTrigger()),
	}}}
	result, err = p.client.Request(ctx, shortcutInterface, "BindShortcuts", []any{session, list, ""}, nil)
	if err != nil {
		return nil, err
	}
	b.description, err = boundDescription(result)
	if err != nil {
		return nil, err
	}
	listenCtx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	go b.listen(listenCtx, capture)
	return b, nil
}

func boundDescription(result map[string]dbus.Variant) (string, error) {
	value, ok := result["shortcuts"]
	if ok {
		var list []portalShortcut
		if dbus.Store([]any{value.Value()}, &list) == nil {
			for _, shortcut := range list {
				if description, ok := shortcut.Properties["trigger_description"].Value().(string); shortcut.ID == captureID && ok && strings.TrimSpace(description) != "" {
					return description, nil
				}
			}
		}
	}
	return "", errors.New("桌面门户没有为截图操作分配快捷键，原绑定已保留")
}

func (b *portalBinding) listen(ctx context.Context, capture func(string)) {
	for {
		select {
		case <-ctx.Done():
			return
		case signal, ok := <-b.sub.Signals:
			if !ok {
				return
			}
			var session dbus.ObjectPath
			var id string
			var timestamp uint64
			var options map[string]dbus.Variant
			if dbus.Store(signal.Body, &session, &id, &timestamp, &options) != nil || session != b.session || id != captureID || !b.active.Load() {
				continue
			}
			token, _ := options["activation_token"].Value().(string)
			b.dispatch(func() {
				if b.active.Load() {
					capture(token)
				}
			})
		}
	}
}

func (b *portalBinding) Activate()           { b.active.Store(true) }
func (b *portalBinding) Description() string { return b.description }
func (b *portalBinding) Close() {
	b.once.Do(func() {
		b.active.Store(false)
		if b.cancel != nil {
			b.cancel()
		}
		if b.sub != nil {
			b.sub.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = b.client.Call(ctx, portal.Destination, b.session, "org.freedesktop.portal.Session.Close").Err
	})
}

func (p *portalProvider) Close() {
	if p.client != nil {
		_ = p.client.Close()
		p.client = nil
	}
}
