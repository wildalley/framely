package translate

import (
	"context"
	"path/filepath"
	"sync"
)

type modelOperation struct {
	gate  chan struct{}
	users int
}

// modelCoordinator owns per-directory operation gates and their references.
// It coordinates windows without coupling the HTTP/file code to UI lifetime.
// The zero value is ready to use.
type modelCoordinator struct {
	mu     sync.Mutex
	active map[string]*modelOperation
}

var modelOperations modelCoordinator

func lockModel(ctx context.Context, root string, m Model) (func(), error) {
	key, err := filepath.Abs(filepath.Join(root, m.Direction))
	if err != nil {
		return nil, err
	}
	return modelOperations.lock(ctx, key)
}

// lock queues on one model directory. Cancellation never waits for another
// HTTP request; unused entries are removed instead of retaining old roots.
// The returned unlock function must be called exactly once.
func (c *modelCoordinator) lock(ctx context.Context, key string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.active == nil {
		c.active = make(map[string]*modelOperation)
	}
	op := c.active[key]
	if op == nil {
		op = &modelOperation{gate: make(chan struct{}, 1)}
		c.active[key] = op
	}
	op.users++
	c.mu.Unlock()
	forget := func() {
		c.mu.Lock()
		op.users--
		if op.users == 0 {
			delete(c.active, key)
		}
		c.mu.Unlock()
	}
	select {
	case op.gate <- struct{}{}:
		unlock := func() { <-op.gate; forget() }
		if err := ctx.Err(); err != nil {
			unlock()
			return nil, err
		}
		return unlock, nil
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	}
}
