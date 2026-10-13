package llm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// Logf is used for chain diagnostics (fallbacks, first-time skips). Services may replace it.
var Logf = func(format string, args ...any) { log.Printf(format, args...) }

// Debugf is used for repeated, low-value diagnostics. Default: discard.
var Debugf = func(format string, args ...any) {}

// skip notices are logged via Logf only once per (provider, fn, reason) per process;
// repeats go to Debugf, so a seeded-but-unregistered provider doesn't spam per call.
var skipSeen sync.Map

func logSkip(provider string, fn Function, tenantID, reason string) {
	key := provider + "|" + string(fn) + "|" + reason
	if _, dup := skipSeen.LoadOrStore(key, struct{}{}); dup {
		Debugf("llm: provider %q in chain fn=%s tenant=%s is %s; skipping", provider, fn, tenantID, reason)
		return
	}
	Logf("llm: provider %q in chain fn=%s tenant=%s is %s; skipping (further occurrences at debug level)", provider, fn, tenantID, reason)
}

// Availability is optionally implemented by providers that can be registered but unusable
// (e.g. Gemini without an API key). Unavailable providers are skipped.
type Availability interface{ Available() bool }

// RetryClassifier is optionally implemented by providers with provider-specific transient
// error detection (e.g. Anthropic SDK errors). It is OR-ed with the registry classifier.
type RetryClassifier interface{ IsRetryable(err error) bool }

// Registry maps provider ids to implementations and resolves the chain for a function.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	Resolver  *Resolver
	// RetryClassifier decides whether an error moves to the next chain link.
	// nil → IsProviderRetryable. Provider-level RetryClassifier is OR-ed in.
	RetryClassifier func(error) bool
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
	Timeout  time.Duration // from chain timeout_ms; 0 = none
}

// Candidates resolves the chain for (tenant, fn) and returns usable links in order.
// If none of the configured links is usable, the code default chain (Anthropic) is used.
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
		logSkip("<chain>", fn, tenantID, "empty after filtering, falling back to code default "+def.Chain[0].Provider)
		out = g.usable(def.Chain, fn, tenantID)
	}
	return out
}

func (g *Registry) usable(chain []ChainLink, fn Function, tenantID string) []Candidate {
	var out []Candidate
	for _, l := range chain {
		p, ok := g.Get(l.Provider)
		if !ok {
			logSkip(l.Provider, fn, tenantID, "not registered")
			continue
		}
		if a, ok := p.(Availability); ok && !a.Available() {
			logSkip(l.Provider, fn, tenantID, "unavailable (no key?)")
			continue
		}
		var to time.Duration
		if l.TimeoutMS > 0 {
			to = time.Duration(l.TimeoutMS) * time.Millisecond
		}
		out = append(out, Candidate{Provider: p, Model: l.Model, Timeout: to})
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
func IsRetryableFor(p Provider, err error) bool { return isRetryable(nil, p, err) }

func isRetryable(classify func(error) bool, p Provider, err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if classify == nil {
		classify = IsProviderRetryable
	}
	if classify(err) {
		return true
	}
	if rc, ok := p.(RetryClassifier); ok {
		return rc.IsRetryable(err)
	}
	return false
}

// RunResult reports which link actually served the call.
type RunResult struct {
	Provider string // provider id of the link that succeeded (or last tried on error)
	Model    string // model as configured on that link ("" = provider default)
	Attempts int    // links tried
}

// Run executes call over the fallback chain (BE-05). Each link gets its own ctx bounded by
// timeout_ms. A link timeout or a retryable error moves to the next link; non-retryable
// errors and parent-ctx cancellation return immediately. Returns the last error.
func (g *Registry) Run(ctx context.Context, tenantID string, fn Function,
	call func(ctx context.Context, p Provider, model string) error) (RunResult, error) {
	var res RunResult
	cands := g.Candidates(ctx, tenantID, fn)
	if len(cands) == 0 {
		return res, fmt.Errorf("llm: no registered provider for function %q", fn)
	}
	var err error
	for i, c := range cands {
		res = RunResult{Provider: c.Provider.ID(), Model: c.Model, Attempts: i + 1}
		err = g.runLink(ctx, c, call)
		if err == nil {
			if i > 0 {
				Logf("llm: fn=%s tenant=%s succeeded on fallback link %d (%s)", fn, tenantID, i+1, c.Provider.ID())
			}
			return res, nil
		}
		if ctx.Err() != nil {
			return res, err
		}
		linkTimedOut := c.Timeout > 0 && errors.Is(err, context.DeadlineExceeded)
		if !linkTimedOut && !isRetryable(g.RetryClassifier, c.Provider, err) {
			return res, err
		}
		if i < len(cands)-1 {
			Logf("llm: fn=%s tenant=%s provider %s failed (retryable), trying next: %v", fn, tenantID, c.Provider.ID(), err)
		}
	}
	return res, err
}

func (g *Registry) runLink(ctx context.Context, c Candidate, call func(context.Context, Provider, string) error) error {
	if c.Timeout <= 0 {
		return call(ctx, c.Provider, c.Model)
	}
	lctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	err := call(lctx, c.Provider, c.Model)
	if err != nil && lctx.Err() == context.DeadlineExceeded && !errors.Is(err, context.DeadlineExceeded) {
		// SDKs sometimes wrap deadline errors opaquely; normalise so the chain advances.
		err = fmt.Errorf("%w: %v", context.DeadlineExceeded, err)
	}
	return err
}
