//go:build linux

package shortcuts

import (
	"context"
	"errors"
	"fmt"
	"github.com/wildalley/framely/internal/brand"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/godbus/dbus/v5"
	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/niribind"
	"github.com/wildalley/framely/internal/portal"
	"github.com/wildalley/framely/internal/shortcutkey"
)

const shortcutInterface = "org.freedesktop.portal.GlobalShortcuts"
const captureID = "capture"
const scrollID = "scroll"
const recordID = "record"

func newPlatformProvider() Provider {
	if os.Getenv("XDG_SESSION_TYPE") == "wayland" || os.Getenv("WAYLAND_DISPLAY") != "" {
		return &waylandProvider{}
	}
	return &nativeProvider{}
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

func (p *waylandProvider) PrepareSet(ctx context.Context, cfg config.Config, capture, scroll, record func(string), changed func()) (Binding, error) {
	if niribind.MatchesShortcuts(cfg.CaptureShortcut, cfg.ScrollShortcut, cfg.RecordShortcut) {
		if err := niribind.Verify(ctx); err != nil {
			return nil, err
		}
		set := &bindingSet{capture: &niriBinding{key: cfg.CaptureShortcut}}
		if cfg.ScrollShortcut != "" {
			set.scroll = &niriBinding{key: cfg.ScrollShortcut}
		}
		if cfg.RecordShortcut != "" {
			set.record = &niriBinding{key: cfg.RecordShortcut}
		}
		return set, nil
	}
	return p.portalProvider.PrepareSet(ctx, cfg, capture, scroll, record, changed)
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
	client       *portal.Client
	session      dbus.ObjectPath
	descriptions map[string]string
	mu           sync.RWMutex
	changed      func()
	sub          *portal.Subscription
	cancel       context.CancelFunc
	dispatch     func(func())
	active       atomic.Bool
	once         sync.Once
}

type portalShortcut struct {
	ID         string
	Properties map[string]dbus.Variant
}

func (p *portalProvider) Prepare(ctx context.Context, key string, capture func(string)) (_ Binding, err error) {
	return p.prepare(ctx, config.Config{CaptureShortcut: key}, capture, nil, nil, nil)
}

func (p *portalProvider) PrepareSet(ctx context.Context, cfg config.Config, capture, scroll, record func(string), changed func()) (Binding, error) {
	return p.prepare(ctx, cfg, capture, scroll, record, changed)
}

func (p *portalProvider) prepare(ctx context.Context, cfg config.Config, capture, scroll, record func(string), changed func()) (_ Binding, err error) {
	if p.client == nil {
		if p.client, err = portal.New(); err != nil {
			return nil, err
		}
	}
	parsed, err := shortcutkey.Parse(cfg.CaptureShortcut, "linux")
	if err != nil {
		return nil, err
	}
	result, err := p.client.Request(ctx, shortcutInterface, "CreateSession", nil, map[string]dbus.Variant{
		"session_handle_token": dbus.MakeVariant(fmt.Sprintf("screenpin_%d_%d", os.Getpid(), p.tokens.Add(1))),
	})
	if err != nil {
		if errors.Is(err, portal.ErrFailed) {
			return nil, fmt.Errorf("桌面无法创建全局快捷键会话，请检查当前桌面的门户支持；可在桌面设置中绑定 framely --capture: %w", err)
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
	b := &portalBinding{client: p.client, session: session, dispatch: dispatch, changed: changed}
	defer func() {
		if err != nil {
			b.Close()
		}
	}()
	b.sub, err = p.client.Subscribe(dbus.WithMatchSender(portal.Destination),
		dbus.WithMatchInterface(shortcutInterface), dbus.WithMatchObjectPath(portal.ObjectPath))
	if err != nil {
		return nil, err
	}
	list := []portalShortcut{{ID: captureID, Properties: map[string]dbus.Variant{
		"description":       dbus.MakeVariant("截取屏幕"),
		"preferred_trigger": dbus.MakeVariant(parsed.XDGTrigger()),
	}}}
	// Stable action IDs allow the desktop to retain user assignments. Include
	// both actions on startup so ConfigureShortcuts can assign either later.
	if scroll != nil {
		key := cfg.ScrollShortcut
		if key == "" {
			key = config.DefaultScrollShortcut
		}
		parsed, parseErr := shortcutkey.Parse(key, "linux")
		if parseErr != nil {
			return nil, parseErr
		}
		list = append(list, portalShortcut{ID: scrollID, Properties: map[string]dbus.Variant{
			"description":       dbus.MakeVariant("长截图（再次按下完成）"),
			"preferred_trigger": dbus.MakeVariant(parsed.XDGTrigger()),
		}})
	}
	if record != nil {
		properties := map[string]dbus.Variant{"description": dbus.MakeVariant("录屏（再次按下结束）")}
		if cfg.RecordShortcut != "" {
			key, e := shortcutkey.Parse(cfg.RecordShortcut, "linux")
			if e != nil {
				return nil, e
			}
			properties["preferred_trigger"] = dbus.MakeVariant(key.XDGTrigger())
		}
		list = append(list, portalShortcut{ID: recordID, Properties: properties})
	}
	result, err = p.client.Request(ctx, shortcutInterface, "BindShortcuts", []any{session, list, ""}, nil)
	if err != nil {
		return nil, err
	}
	var assigned []portalShortcut
	if value, ok := result["shortcuts"]; !ok || dbus.Store([]any{value.Value()}, &assigned) != nil {
		return nil, errors.New("快捷键门户返回了无效的按键列表")
	}
	b.setDescriptions(assigned)
	if scroll == nil {
		if _, err = boundDescription(result); err != nil {
			return nil, err
		}
	}
	listenCtx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	go b.listen(listenCtx, capture, scroll, record)
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

func (b *portalBinding) listen(ctx context.Context, capture, scroll, record func(string)) {
	for {
		select {
		case <-ctx.Done():
			return
		case signal, ok := <-b.sub.Signals:
			if !ok {
				return
			}
			var session dbus.ObjectPath
			if signal.Name == shortcutInterface+".ShortcutsChanged" {
				var assigned []portalShortcut
				if dbus.Store(signal.Body, &session, &assigned) == nil && session == b.session {
					b.setDescriptions(assigned)
					if b.changed != nil {
						b.dispatch(func() {
							if b.active.Load() {
								b.changed()
							}
						})
					}
				}
				continue
			}
			if signal.Name != shortcutInterface+".Activated" {
				continue
			}
			var id string
			var timestamp uint64
			var options map[string]dbus.Variant
			if dbus.Store(signal.Body, &session, &id, &timestamp, &options) != nil || session != b.session || !b.active.Load() {
				continue
			}
			action := capture
			if id == scrollID {
				action = scroll
			} else if id == recordID {
				action = record
			} else if id != captureID {
				continue
			}
			if action == nil {
				continue
			}
			token, _ := options["activation_token"].Value().(string)
			b.dispatch(func() {
				if b.active.Load() {
					action(token)
				}
			})
		}
	}
}

func (b *portalBinding) Activate()                 { b.active.Store(true) }
func (b *portalBinding) Description() string       { return b.descriptionFor(captureID) }
func (b *portalBinding) RecordDescription() string { return b.descriptionFor(recordID) }
func (b *portalBinding) ScrollDescription() string { return b.descriptionFor(scrollID) }
func (b *portalBinding) descriptionFor(id string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.descriptions[id]
}
func (b *portalBinding) setDescriptions(shortcuts []portalShortcut) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.descriptions = make(map[string]string)
	for _, shortcut := range shortcuts {
		if s, ok := shortcut.Properties["trigger_description"].Value().(string); ok {
			b.descriptions[shortcut.ID] = strings.TrimSpace(s)
		}
	}
}

func (b *portalBinding) Configure(ctx context.Context) error {
	var version dbus.Variant
	if err := b.client.Call(ctx, portal.Destination, portal.ObjectPath,
		"org.freedesktop.DBus.Properties.Get", shortcutInterface, "version").Store(&version); err != nil {
		return err
	}
	if v, ok := version.Value().(uint32); !ok || v < 2 {
		return errors.New("当前桌面不支持应用内打开快捷键配置，请在系统键盘设置中修改 " + brand.Name + " 的快捷键")
	}
	// ConfigureShortcuts returns no request handle: assignment changes arrive
	// through ShortcutsChanged, not through Request.Response.
	return b.client.Call(ctx, portal.Destination, portal.ObjectPath,
		shortcutInterface+".ConfigureShortcuts", b.session, "", map[string]dbus.Variant{}).Err
}
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
