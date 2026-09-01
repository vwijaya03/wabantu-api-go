package retrieval

import (
	"context"
	"sync"
	"time"
)

// Budget limits concurrent / rate-limited retrieval calls per process.
type Budget struct {
	mu      sync.Mutex
	max     int
	inUse   int
	waiters int
}

func NewBudget(maxConcurrent int) *Budget {
	if maxConcurrent <= 0 {
		maxConcurrent = 8
	}
	return &Budget{max: maxConcurrent}
}

func (b *Budget) Acquire(ctx context.Context) error {
	b.mu.Lock()
	for b.inUse >= b.max {
		b.waiters++
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			b.mu.Lock()
			b.waiters--
			b.mu.Unlock()
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
		b.mu.Lock()
		b.waiters--
	}
	b.inUse++
	b.mu.Unlock()
	return nil
}

func (b *Budget) Release() {
	b.mu.Lock()
	if b.inUse > 0 {
		b.inUse--
	}
	b.mu.Unlock()
}

// WithBudget runs fn while holding a budget slot.
func WithBudget(ctx context.Context, b *Budget, fn func() error) error {
	if b == nil {
		return fn()
	}
	if err := b.Acquire(ctx); err != nil {
		return err
	}
	defer b.Release()
	return fn()
}
