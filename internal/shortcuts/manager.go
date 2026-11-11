// Package shortcuts replaces capture bindings only after registration and
// configuration persistence have both succeeded.
package shortcuts

import (
	"context"
	"errors"
	"runtime"
	"sync"

	"github.com/wildalley/framely/internal/config"
	"github.com/wildalley/framely/internal/shortcutkey"
)

type Binding interface {
	Activate()
	Close()
	Description() string
}

type Provider interface {
	Prepare(context.Context, string, func(string)) (Binding, error)
	Close()
}

// A portal owns one session for all actions; its assignments are controlled
// by the desktop, rather than by re-registering a preferred trigger.
type setProvider interface {
	PrepareSet(context.Context, config.Config, func(string), func(string), func(string), func()) (Binding, error)
}

type systemBinding interface {
	Configure(context.Context) error
}

type bindingSet struct{ capture, scroll, record Binding }

func (b *bindingSet) Activate() {
	b.capture.Activate()
	if b.record != nil {
		b.record.Activate()
	}
	if b.scroll != nil {
		b.scroll.Activate()
	}
}
func (b *bindingSet) Close() {
	b.capture.Close()
	if b.record != nil {
		b.record.Close()
	}
	if b.scroll != nil {
		b.scroll.Close()
	}
}
func (b *bindingSet) Description() string { return b.capture.Description() }
func (b *bindingSet) ScrollDescription() string {
	if b.scroll != nil {
		return b.scroll.Description()
	}
	return ""
}

func (b *bindingSet) RecordDescription() string {
	if b.record != nil {
		return b.record.Description()
	}
	return ""
}

type Manager struct {
	mu       sync.Mutex
	provider Provider
	capture  func(string)
	scroll   func(string)
	record   func(string)
	changed  func()
	save     func(config.Config) error
	active   Binding
	key      string
	closed   bool
}

var errClosed = errors.New("快捷键管理器已关闭")

func New(capture, scroll, record func(string), changed func()) *Manager {
	return &Manager{provider: newPlatformProvider(), capture: capture, scroll: scroll, record: record, changed: changed, save: config.Config.Save}
}

func normalize(cfg config.Config) (config.Config, string, error) {
	if err := cfg.Validate(); err != nil {
		return cfg, "", err
	}
	capture, _ := shortcutkey.Parse(cfg.CaptureShortcut, runtime.GOOS)
	cfg.CaptureShortcut = capture.Canonical()
	if cfg.ScrollShortcut != "" {
		scroll, _ := shortcutkey.Parse(cfg.ScrollShortcut, runtime.GOOS)
		cfg.ScrollShortcut = scroll.Canonical()
	}
	if cfg.RecordShortcut != "" {
		key, _ := shortcutkey.Parse(cfg.RecordShortcut, runtime.GOOS)
		cfg.RecordShortcut = key.Canonical()
	}
	return cfg, cfg.CaptureShortcut + "\n" + cfg.ScrollShortcut + "\n" + cfg.RecordShortcut, nil
}

func (m *Manager) prepare(ctx context.Context, cfg config.Config) (Binding, error) {
	if p, ok := m.provider.(setProvider); ok {
		return p.PrepareSet(ctx, cfg, m.capture, m.scroll, m.record, m.changed)
	}
	capture, err := m.provider.Prepare(ctx, cfg.CaptureShortcut, m.capture)
	if err != nil {
		return nil, err
	}
	set := &bindingSet{capture: capture}
	for _, action := range []struct {
		key      string
		callback func(string)
		target   *Binding
	}{{cfg.ScrollShortcut, m.scroll, &set.scroll}, {cfg.RecordShortcut, m.record, &set.record}} {
		if action.key == "" {
			continue
		}
		binding, err := m.provider.Prepare(ctx, action.key, action.callback)
		if err != nil {
			set.Close()
			return nil, err
		}
		*action.target = binding
	}
	return set, nil
}

// Start restores a saved binding without rewriting its configuration.
func (m *Manager) Start(ctx context.Context, cfg config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, key, err := normalize(cfg)
	if err != nil {
		return err
	}
	next, err := m.prepare(ctx, cfg)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		next.Close()
		return err
	}
	previous := m.active
	m.active, m.key = next, key
	next.Activate()
	if previous != nil {
		previous.Close()
	}
	return nil
}

// Apply keeps the old binding alive while a new one waits for permission.
// A failed save closes only the candidate, leaving the old binding intact.
func (m *Manager) Apply(ctx context.Context, cfg config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, key, err := normalize(cfg)
	if err != nil {
		return err
	}
	var next Binding
	if m.active != nil && key != m.key {
		if _, ok := m.active.(systemBinding); ok {
			return errors.New("当前桌面管理快捷键，请使用“配置系统快捷键”修改；实际按键以系统分配为准")
		}
	}
	if m.active == nil || key != m.key {
		if next, err = m.prepare(ctx, cfg); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		if next != nil {
			next.Close()
		}
		return err
	}
	if err = m.save(cfg); err != nil {
		if next != nil {
			next.Close()
		}
		return err
	}
	if next != nil {
		previous := m.active
		m.active, m.key = next, key
		next.Activate()
		if previous != nil {
			previous.Close()
		}
	}
	return nil
}

func (m *Manager) ScrollDescription() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.active.(interface{ ScrollDescription() string }); ok {
		return b.ScrollDescription()
	}
	return ""
}

func (m *Manager) RecordDescription() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.active.(interface{ RecordDescription() string }); ok {
		return b.RecordDescription()
	}
	return ""
}

func (m *Manager) SystemManaged() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.active.(systemBinding)
	return ok
}

func (m *Manager) Configure(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errClosed
	}
	if b, ok := m.active.(systemBinding); ok {
		return b.Configure(ctx)
	}
	return errors.New("当前快捷键不由系统门户管理")
}

func (m *Manager) Description() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		return m.active.Description()
	}
	return ""
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	if m.active != nil {
		m.active.Close()
		m.active = nil
	}
	m.provider.Close()
}
