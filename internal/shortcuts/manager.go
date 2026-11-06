// Package shortcuts replaces capture bindings only after registration and
// configuration persistence have both succeeded.
package shortcuts

import (
	"context"
	"errors"
	"runtime"
	"sync"

	"github.com/wildalley/screenpin/internal/config"
	"github.com/wildalley/screenpin/internal/shortcutkey"
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

type Manager struct {
	mu       sync.Mutex
	provider Provider
	capture  func(string)
	save     func(config.Config) error
	active   Binding
	key      string
	closed   bool
}

var errClosed = errors.New("快捷键管理器已关闭")

func New(capture func(string)) *Manager {
	return &Manager{provider: newPlatformProvider(), capture: capture, save: config.Config.Save}
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
	key, err := shortcutkey.Parse(cfg.CaptureShortcut, runtime.GOOS)
	if err != nil {
		return err
	}
	next, err := m.provider.Prepare(ctx, key.Canonical(), m.capture)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		next.Close()
		return err
	}
	previous := m.active
	m.active, m.key = next, key.Canonical()
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
	key, err := shortcutkey.Parse(cfg.CaptureShortcut, runtime.GOOS)
	if err != nil {
		return err
	}
	var next Binding
	if m.active == nil || key.Canonical() != m.key {
		if next, err = m.provider.Prepare(ctx, key.Canonical(), m.capture); err != nil {
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
		m.active, m.key = next, key.Canonical()
		next.Activate()
		if previous != nil {
			previous.Close()
		}
	}
	return nil
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
