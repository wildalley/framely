package shortcuts

import (
	"context"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/wildalley/screenpin/internal/config"
)

type nativeProvider struct{}

type nativeBinding struct {
	key    string
	active atomic.Bool
}

func (nativeProvider) Prepare(ctx context.Context, key string, capture func(string)) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b := &nativeBinding{key: key}
	if err := mygo.GlobalShortcut.Register(key, func() {
		if b.active.Load() {
			capture("")
		}
	}); err != nil {
		return nil, err
	}
	return b, nil
}

func (nativeProvider) Close()                {}
func (b *nativeBinding) Activate()           { b.active.Store(true) }
func (b *nativeBinding) Description() string { return config.DisplayShortcut(b.key) }
func (b *nativeBinding) Close() {
	b.active.Store(false)
	mygo.GlobalShortcut.Unregister(b.key)
}
