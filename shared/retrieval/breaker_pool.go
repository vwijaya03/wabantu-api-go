package retrieval

import (
	"sync"
	"time"
)

const (
	breakerPoolMaxEntries = 500
	breakerPoolIdleTTL    = time.Hour
)

type breakerEntry struct {
	cb       *CircuitBreaker
	lastUsed time.Time
}

// BreakerPool holds per-tenant circuit breakers so one tenant cannot trip retrieval for all.
type BreakerPool struct {
	mu        sync.Mutex
	breakers  map[string]*breakerEntry
	global    *CircuitBreaker
	threshold int
	cooldown  time.Duration
}

func NewBreakerPool(threshold int, cooldown time.Duration) *BreakerPool {
	return &BreakerPool{
		breakers:  make(map[string]*breakerEntry),
		global:    NewCircuitBreaker(threshold, cooldown),
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
	p.evictIdleLocked(time.Now())
	if len(p.breakers) >= breakerPoolMaxEntries && tenantID != "_global" {
		return p.global
	}
	ent, ok := p.breakers[tenantID]
	if !ok {
		ent = &breakerEntry{cb: NewCircuitBreaker(p.threshold, p.cooldown), lastUsed: time.Now()}
		p.breakers[tenantID] = ent
	}
	ent.lastUsed = time.Now()
	return ent.cb
}

func (p *BreakerPool) evictIdleLocked(now time.Time) {
	cutoff := now.Add(-breakerPoolIdleTTL)
	for id, ent := range p.breakers {
		if ent.lastUsed.Before(cutoff) {
			delete(p.breakers, id)
		}
	}
}

// AnyOpenRecently reports whether any breaker in the pool opened within the window.
func (p *BreakerPool) AnyOpenRecently(within time.Duration) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if p.global != nil && p.global.OpenedWithin(now, within) {
		return true
	}
	for _, ent := range p.breakers {
		if ent.cb != nil && ent.cb.OpenedWithin(now, within) {
			return true
		}
	}
	return false
}
