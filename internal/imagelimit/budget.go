package imagelimit

import (
	"errors"
	"sync"
)

var ErrBudget = errors.New("图片内存预算不足，请关闭部分图片后重试")

// Pictures accounts for queued decodes, open pictures and their active tasks.
// This is an image working-set estimate, not a process RSS or GPU memory cap.
var Pictures = NewBudget(PictureBudgetBytes)

type Budget struct {
	mu          sync.Mutex
	limit, used int64
}
type reservation struct {
	budget *Budget
	bytes  int64
	refs   int
}

// A Lease is one owner's handle. Copies of the pointer share an idempotent
// release; Retain creates a separate handle for an independent lifetime.
type Lease struct {
	reservation *reservation
	released    bool
}

func NewBudget(limit int64) *Budget { return &Budget{limit: limit} }

func (b *Budget) Acquire(bytes int64) (*Lease, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if bytes <= 0 || bytes > b.limit-b.used {
		return nil, ErrBudget
	}
	b.used += bytes
	return &Lease{reservation: &reservation{budget: b, bytes: bytes, refs: 1}}, nil
}

func (b *Budget) Used() int64 { b.mu.Lock(); defer b.mu.Unlock(); return b.used }

func (l *Lease) Retain() *Lease {
	if l == nil {
		return nil
	}
	r := l.reservation
	r.budget.mu.Lock()
	defer r.budget.mu.Unlock()
	if l.released {
		return nil
	}
	r.refs++
	return &Lease{reservation: r}
}

func (l *Lease) Release() {
	if l == nil {
		return
	}
	r := l.reservation
	r.budget.mu.Lock()
	defer r.budget.mu.Unlock()
	if l.released {
		return
	}
	l.released = true
	r.refs--
	if r.refs == 0 {
		r.budget.used -= r.bytes
	}
}

// ReduceTo releases unused headroom atomically, without an admission gap in
// which another window could take the space reserved for a completed capture.
func (l *Lease) ReduceTo(bytes int64) bool {
	if l == nil {
		return false
	}
	r := l.reservation
	r.budget.mu.Lock()
	defer r.budget.mu.Unlock()
	if l.released || bytes <= 0 || bytes > r.bytes {
		return false
	}
	r.budget.used -= r.bytes - bytes
	r.bytes = bytes
	return true
}

// Allow for the original RGBA, display copy and one render/decode buffer.
// An extra byte per pixel covers mosaic generation, plus fixed bookkeeping.
func PictureBytes(w, h int) int64 { return int64(w)*int64(h)*13 + (64 << 10) }
