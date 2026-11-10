package shortcuts

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/wildalley/screenpin/internal/config"
)

// A candidate may retain an unchanged key, or swap the two actions. Share
// the registration until persistence succeeds and the previous owners close.
type nativeProvider struct {
	mu         sync.Mutex
	bindings   map[string][]*nativeBinding
	register   func(string, func()) error
	unregister func(string)
}
type nativeBinding struct {
	provider *nativeProvider
	key      string
	capture  func(string)
	active   atomic.Bool
	once     sync.Once
}

func (p *nativeProvider) Prepare(ctx context.Context, key string, capture func(string)) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	if p.register == nil {
		p.register = mygo.GlobalShortcut.Register
		p.unregister = mygo.GlobalShortcut.Unregister
	}
	if p.bindings == nil {
		p.bindings = make(map[string][]*nativeBinding)
	}
	b := &nativeBinding{provider: p, key: key, capture: capture}
	exists := len(p.bindings[key]) > 0
	p.mu.Unlock()
	if !exists {
		if err := p.register(key, func() { p.fire(key) }); err != nil {
			return nil, err
		}
	}
	p.mu.Lock()
	p.bindings[key] = append(p.bindings[key], b)
	p.mu.Unlock()
	return b, nil
}
func (p *nativeProvider) fire(key string) {
	p.mu.Lock()
	var action func(string)
	for _, b := range p.bindings[key] {
		if b.active.Load() {
			action = b.capture
			break
		}
	}
	p.mu.Unlock()
	if action != nil {
		action("")
	}
}
func (p *nativeProvider) Close()             {}
func (b *nativeBinding) Activate()           { b.active.Store(true) }
func (b *nativeBinding) Description() string { return config.DisplayShortcut(b.key) }
func (b *nativeBinding) Close() {
	b.once.Do(func() {
		b.active.Store(false)
		p := b.provider
		p.mu.Lock()
		owners := p.bindings[b.key]
		for i, owner := range owners {
			if owner == b {
				owners = append(owners[:i], owners[i+1:]...)
				break
			}
		}
		if len(owners) == 0 {
			delete(p.bindings, b.key)
		} else {
			p.bindings[b.key] = owners
		}
		p.mu.Unlock()
		if len(owners) == 0 {
			p.unregister(b.key)
		}
	})
}
