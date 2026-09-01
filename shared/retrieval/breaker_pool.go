package retrieval

import (
	"sync"
	"time"
)

// BreakerPool holds per-tenant circuit breakers so one tenant cannot trip retrieval for all.
type BreakerPool struct {
	mu        sync.Mutex
	breakers  map[string]*CircuitBreaker
	threshold int
	cooldown  time.Duration
}

func NewBreakerPool(threshold int, cooldown time.Duration) *BreakerPool {
	return &BreakerPool{
		breakers:  make(map[string]*CircuitBreaker),
		threshold: threshold,
		cooldown:  cooldown,
	}
}

func (p *BreakerPool) For(tenantID string) *CircuitBreaker {
	if p == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "_global"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	cb, ok := p.breakers[tenantID]
	if !ok {
		cb = NewCircuitBreaker(p.threshold, p.cooldown)
		p.breakers[tenantID] = cb
	}
	return cb
}
