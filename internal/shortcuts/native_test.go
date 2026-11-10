package shortcuts

import (
	"context"
	"errors"
	"testing"

	"github.com/wildalley/screenpin/internal/config"
)

func TestNativeActionsReuseKeysAndSwapWithoutDoubleRegistration(t *testing.T) {
	handlers := map[string]func(){}
	p := &nativeProvider{register: func(key string, fn func()) error {
		if handlers[key] != nil {
			return errors.New("already registered")
		}
		handlers[key] = fn
		return nil
	}, unregister: func(key string) { delete(handlers, key) }}
	captures, scrolls := 0, 0
	m := &Manager{provider: p, capture: func(string) { captures++ }, scroll: func(string) { scrolls++ }, save: func(config.Config) error { return nil }}
	defer m.Close()
	cfg := config.Default()
	if err := m.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	cfg.ScrollShortcut = config.DefaultScrollShortcut
	if err := m.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	handlers[config.DefaultShortcut]()
	handlers[config.DefaultScrollShortcut]()
	if captures != 1 || scrolls != 1 {
		t.Fatal("actions were not separate")
	}
	cfg.CaptureShortcut, cfg.ScrollShortcut = cfg.ScrollShortcut, cfg.CaptureShortcut
	m.save = func(config.Config) error { return errors.New("disk full") }
	if err := m.Apply(context.Background(), cfg); err == nil {
		t.Fatal("save failure ignored")
	}
	handlers[config.DefaultShortcut]()
	handlers[config.DefaultScrollShortcut]()
	if captures != 2 || scrolls != 2 || m.Description() != config.DisplayShortcut(config.DefaultShortcut) {
		t.Fatal("failed save changed the binding")
	}
	m.save = func(config.Config) error { return nil }
	if err := m.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	handlers[config.DefaultShortcut]()
	if captures != 2 || scrolls != 3 {
		t.Fatal("swapped capture key did not become scroll")
	}
	cfg.ScrollShortcut = ""
	if err := m.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if len(handlers) != 1 || m.ScrollDescription() != "" {
		t.Fatal("clearing scroll retained a registration")
	}
	m.Close()
	if len(handlers) != 0 {
		t.Fatal("closing left registered keys")
	}
}

func TestNativeRecordingShortcutConflictAndRollback(t *testing.T) {
	handlers := map[string]func(){}
	p := &nativeProvider{register: func(key string, fn func()) error {
		if handlers[key] != nil {
			return errors.New("in use")
		}
		handlers[key] = fn
		return nil
	}, unregister: func(key string) { delete(handlers, key) }}
	records := 0
	m := &Manager{provider: p, capture: func(string) {}, scroll: func(string) {}, record: func(string) { records++ }, save: func(config.Config) error { return nil }}
	defer m.Close()
	cfg := config.Default()
	cfg.ScrollShortcut = config.DefaultScrollShortcut
	cfg.RecordShortcut = config.DefaultRecordShortcut
	if err := m.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	handlers[cfg.RecordShortcut]()
	if records != 1 {
		t.Fatal("record action was not dispatched")
	}
	conflict := cfg
	conflict.RecordShortcut = cfg.ScrollShortcut
	if err := m.Apply(context.Background(), conflict); err == nil {
		t.Fatal("duplicate record action accepted")
	}
	cfg.RecordShortcut = "Ctrl+Alt+R"
	m.save = func(config.Config) error { return errors.New("disk full") }
	if err := m.Apply(context.Background(), cfg); err == nil {
		t.Fatal("save failure ignored")
	}
	if handlers[config.DefaultRecordShortcut] == nil || handlers[cfg.RecordShortcut] != nil {
		t.Fatal("record shortcut not rolled back")
	}
	handlers[config.DefaultRecordShortcut]()
	if records != 2 {
		t.Fatal("rollback lost action")
	}
	m.Close()
	if len(handlers) != 0 {
		t.Fatal("shortcut registrations leaked")
	}
}
