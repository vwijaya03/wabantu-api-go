package llm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
)

// Logf is used for chain diagnostics (skipped/unregistered providers, fallbacks).
// Services may replace it (e.g. with rlog) at startup.
var Logf = func(format string, args ...any) { log.Printf(format, args...) }

// Availability is optionally implemented by providers that can be registered but unusable
// (e.g. Gemini without an API key). Unavailable providers are skipped and logged.
type Availability interface{ Available() bool }

// RetryClassifier is optionally implemented by providers with provider-specific transient
// error detection (e.g. Anthropic SDK errors). It is OR-ed with IsProviderRetryable.
type RetryClassifier interface{ IsRetryable(err error) bool }

// Registry maps provider ids to implementations and resolves the chain for a function.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	Resolver  *Resolver
}

func NewRegistry(r *Resolver) *Registry {
	return &Registry{providers: map[string]Provider{}, Resolver: r}
}

func (g *Registry) Register(p Provider) {
	g.mu.Lock()
	g.providers[p.ID()] = p
	g.mu.Unlock()
}

func (g *Registry) Get(id string) (Provider, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	p, ok := g.providers[id]
	return p, ok
}

// Candidate is a usable chain link.
type Candidate struct {
	Provider Provider
	Model    string
}

// Candidates resolves the chain for (tenant, fn) and returns usable links in order.
// Unregistered/unavailable providers are logged, not silently dropped. If none of the
// configured links is usable, the code default chain (Anthropic) is appended as a safety net.
func (g *Registry) Candidates(ctx context.Context, tenantID string, fn Function) []Candidate {
	cfg := CodeDefaults(fn)
	if g.Resolver != nil {
		c, err := g.Resolver.Resolve(ctx, fn, tenantID)
		if err != nil {
			Logf("llm: config resolve failed fn=%s tenant=%s, using code default: %v", fn, tenantID, err)
		}
		cfg = c
	}
	out := g.usable(cfg.Chain, fn, tenantID)
	if len(out) == 0 {
		def := CodeDefaults(fn)
		Logf("llm: no usable provider in configured chain fn=%s tenant=%s, falling back to code default %s", fn, tenantID, def.Chain[0].Provider)
		out = g.usable(def.Chain, fn, tenantID)
	}
	return out
}

func (g *Registry) usable(chain []ChainLink, fn Function, tenantID string) []Candidate {
	var out []Candidate
	for _, l := range chain {
		p, ok := g.Get(l.Provider)
		if !ok {
			Logf("llm: provider %q in chain fn=%s tenant=%s is not registered; skipping", l.Provider, fn, tenantID)
			continue
		}
		if a, ok := p.(Availability); ok && !a.Available() {
			Logf("llm: provider %q in chain fn=%s tenant=%s is unavailable (no key?); skipping", l.Provider, fn, tenantID)
			continue
		}
		out = append(out, Candidate{Provider: p, Model: l.Model})
	}
	return out
}

// For returns the first usable provider+model (no fallback execution).
func (g *Registry) For(ctx context.Context, tenantID string, fn Function) (Provider, string, error) {
	c := g.Candidates(ctx, tenantID, fn)
	if len(c) == 0 {
		return nil, "", fmt.Errorf("llm: no registered provider for function %q", fn)
	}
	return c[0].Provider, c[0].Model, nil
}

// IsRetryableFor combines the neutral check with the provider's own classifier.
func IsRetryableFor(p Provider, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if IsProviderRetryable(err) {
		return true
	}
	if rc, ok := p.(RetryClassifier); ok {
		return rc.IsRetryable(err)
	}
	return false
}

// Run executes call over the fallback chain (BE-05): on a retryable error the next link is
// tried; non-retryable errors (validation/auth) return immediately. Returns the last error.
func (g *Registry) Run(ctx context.Context, tenantID string, fn Function, call func(p Provider, model string) error) error {
	cands := g.Candidates(ctx, tenantID, fn)
	if len(cands) == 0 {
		return fmt.Errorf("llm: no registered provider for function %q", fn)
	}
	var err error
	for i, c := range cands {
		err = call(c.Provider, c.Model)
		if err == nil {
			if i > 0 {
				Logf("llm: fn=%s tenant=%s succeeded on fallback link %d (%s)", fn, tenantID, i+1, c.Provider.ID())
			}
			return nil
		}
		if ctx.Err() != nil || !IsRetryableFor(c.Provider, err) {
			return err
		}
		if i < len(cands)-1 {
			Logf("llm: fn=%s tenant=%s provider %s failed (retryable), trying next: %v", fn, tenantID, c.Provider.ID(), err)
		}
	}
	return err
}
