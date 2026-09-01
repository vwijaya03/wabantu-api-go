package retrieval

import (
	"sync"
	"time"
)

// CircuitBreaker trips open after consecutive failures; half-open after cooldown.
type CircuitBreaker struct {
	mu               sync.Mutex
	failures         int
	threshold        int
	cooldown         time.Duration
	openUntil        time.Time
	lastFailure      error
	halfOpenInFlight bool
}

func NewCircuitBreaker(threshold int, cooldown time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 5
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &CircuitBreaker{threshold: threshold, cooldown: cooldown}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.openUntil.IsZero() || time.Now().After(cb.openUntil) {
		if !cb.openUntil.IsZero() && time.Now().After(cb.openUntil) {
			cb.failures = 0
			cb.openUntil = time.Time{}
		}
		return true
	}
	return false
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.openUntil = time.Time{}
	cb.lastFailure = nil
	cb.halfOpenInFlight = false
}

func (cb *CircuitBreaker) RecordFailure(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.lastFailure = err
	if cb.failures >= cb.threshold {
		cb.openUntil = time.Now().Add(cb.cooldown)
	}
}

func (cb *CircuitBreaker) Open() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return !cb.openUntil.IsZero() && time.Now().Before(cb.openUntil)
}

func (cb *CircuitBreaker) LastError() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.lastFailure
}
