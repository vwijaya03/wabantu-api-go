package llm

import (
	"context"
	"fmt"
	"sync"
)

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

// For returns the first registered provider+model in the resolved chain (fallback chain execution is BE-05).
func (g *Registry) For(ctx context.Context, tenantID string, fn Function) (Provider, string, error) {
	cfg := CodeDefaults(fn)
	if g.Resolver != nil {
		c, _ := g.Resolver.Resolve(ctx, fn, tenantID) // on error Resolve returns code defaults
		cfg = c
	}
	for _, l := range cfg.Chain {
		if p, ok := g.Get(l.Provider); ok {
			return p, l.Model, nil
		}
	}
	return nil, "", fmt.Errorf("llm: no registered provider for function %q", fn)
}
