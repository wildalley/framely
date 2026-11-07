package shortcuts

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/wildalley/screenpin/internal/config"
	"github.com/wildalley/screenpin/internal/portal"
)

type testBinding struct {
	key     string
	capture func(string)
	active  atomic.Bool
	closed  atomic.Bool
}

func (b *testBinding) Activate()           { b.active.Store(true) }
func (b *testBinding) Close()              { b.active.Store(false); b.closed.Store(true) }
func (b *testBinding) Description() string { return b.key }
func (b *testBinding) fire() {
	if b.active.Load() && !b.closed.Load() {
		b.capture("")
	}
}

type testProvider struct {
	bindings []*testBinding
	err      error
	entered  chan struct{}
	release  chan struct{}
}

func (p *testProvider) Prepare(ctx context.Context, key string, capture func(string)) (Binding, error) {
	if p.entered != nil {
		close(p.entered)
		select {
		case <-p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if p.err != nil {
		return nil, p.err
	}
	b := &testBinding{key: key, capture: capture}
	p.bindings = append(p.bindings, b)
	return b, nil
}

func (p *testProvider) Close() {}

func newTestManager(t *testing.T) (*Manager, *testProvider, *atomic.Int32) {
	t.Helper()
	p := &testProvider{}
	captures := &atomic.Int32{}
	m := &Manager{provider: p, capture: func(string) { captures.Add(1) }, save: func(config.Config) error { return nil }}
	if err := m.Start(context.Background(), config.Default()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m, p, captures
}

func TestPermissionCancellationKeepsPreviousBinding(t *testing.T) {
	m, p, captures := newTestManager(t)
	saved := false
	m.save = func(config.Config) error { saved = true; return nil }
	p.err = portal.ErrCancelled
	if err := m.Apply(context.Background(), config.Config{CaptureShortcut: "Ctrl+Shift+A"}); !errors.Is(err, portal.ErrCancelled) {
		t.Fatalf("err=%v", err)
	}
	p.bindings[0].fire()
	if saved || p.bindings[0].closed.Load() || captures.Load() != 1 {
		t.Fatal("cancelled permission replaced the old binding or wrote the configuration")
	}
}

func TestFailedPersistenceClosesOnlyTheCandidate(t *testing.T) {
	m, p, captures := newTestManager(t)
	saveError := errors.New("read-only configuration")
	m.save = func(config.Config) error {
		p.bindings[0].fire()
		p.bindings[1].fire()
		return saveError
	}
	if err := m.Apply(context.Background(), config.Config{CaptureShortcut: "Ctrl+Shift+A"}); !errors.Is(err, saveError) {
		t.Fatalf("err=%v", err)
	}
	if captures.Load() != 1 || !p.bindings[1].closed.Load() || p.bindings[0].closed.Load() {
		t.Fatal("the candidate fired before persistence or the previous binding was removed")
	}
	p.bindings[0].fire()
	if captures.Load() != 2 {
		t.Fatal("the previous binding no longer works")
	}
}

func TestOldBindingWorksWhilePermissionIsPending(t *testing.T) {
	m, p, captures := newTestManager(t)
	p.entered, p.release = make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- m.Apply(context.Background(), config.Config{CaptureShortcut: "Ctrl+Shift+A"}) }()
	<-p.entered
	p.bindings[0].fire()
	if captures.Load() != 1 || p.bindings[0].closed.Load() {
		t.Fatal("waiting for permission disabled the previous binding")
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	p.bindings[0].fire()
	p.bindings[1].fire()
	if captures.Load() != 2 || !p.bindings[0].closed.Load() || m.Description() != "Ctrl+Shift+A" {
		t.Fatal("committing the candidate left duplicate or stale bindings")
	}
}

func TestEquivalentShortcutDoesNotReregister(t *testing.T) {
	m, p, _ := newTestManager(t)
	if err := m.Apply(context.Background(), config.Config{CaptureShortcut: "shift+win+s", HideAfterCopy: true}); err != nil {
		t.Fatal(err)
	}
	if len(p.bindings) != 1 {
		t.Fatal("an equivalent spelling registered the same system keys twice")
	}
}

func TestStartupFailureCanRetryAuthorizationForTheSameShortcut(t *testing.T) {
	p := &testProvider{err: portal.ErrFailed}
	saves, captures := 0, 0
	m := &Manager{provider: p, capture: func(string) { captures++ }, save: func(config.Config) error { saves++; return nil }}
	defer m.Close()
	cfg := config.Default()
	if err := m.Start(context.Background(), cfg); !errors.Is(err, portal.ErrFailed) || m.Description() != "" || saves != 0 {
		t.Fatalf("failed startup was reported as bound: err=%v description=%q saves=%d", err, m.Description(), saves)
	}
	p.err = nil
	if err := m.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	p.bindings[0].fire()
	if len(p.bindings) != 1 || saves != 1 || captures != 1 || m.Description() == "" {
		t.Fatal("retrying the unchanged shortcut did not register and activate it")
	}
}

func TestCancelledSaveDoesNotPersistAndClosedManagerRejectsChanges(t *testing.T) {
	m, p, _ := newTestManager(t)
	saved := false
	m.save = func(config.Config) error { saved = true; return nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Apply(ctx, config.Default()); !errors.Is(err, context.Canceled) || saved {
		t.Fatal("a cancelled save still changed the configuration")
	}
	m.Close()
	if err := m.Apply(context.Background(), config.Default()); !errors.Is(err, errClosed) || len(p.bindings) != 1 {
		t.Fatal("a closed manager accepted another binding")
	}
}
