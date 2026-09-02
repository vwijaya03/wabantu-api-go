package retrieval

import (
	"fmt"
	"testing"
	"time"
)

func TestBreakerPoolEvictsIdleEntries(t *testing.T) {
	t.Parallel()
	pool := NewBreakerPool(2, time.Minute)
	ent := pool.breakers
	_ = ent
	cb := pool.For("idle-tenant")
	if cb == nil {
		t.Fatal("expected breaker")
	}
	pool.mu.Lock()
	if e, ok := pool.breakers["idle-tenant"]; ok {
		e.lastUsed = time.Now().Add(-2 * breakerPoolIdleTTL)
	}
	pool.mu.Unlock()
	pool.For("fresh-tenant")
	pool.mu.Lock()
	if _, ok := pool.breakers["idle-tenant"]; ok {
		t.Fatal("idle tenant breaker should be evicted")
	}
	pool.mu.Unlock()
}

func TestBreakerPoolFallsBackToGlobalAtMaxEntries(t *testing.T) {
	t.Parallel()
	pool := NewBreakerPool(1, time.Hour)
	pool.mu.Lock()
	for i := 0; i < breakerPoolMaxEntries; i++ {
		id := fmt.Sprintf("tenant-%d", i)
		pool.breakers[id] = &breakerEntry{cb: NewCircuitBreaker(1, time.Hour), lastUsed: time.Now()}
	}
	pool.mu.Unlock()
	cb := pool.For("overflow-tenant")
	if cb != pool.global {
		t.Fatal("expected global breaker fallback when pool at capacity")
	}
}
