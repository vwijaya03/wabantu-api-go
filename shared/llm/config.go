package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ChainLink is one entry of ai_function_config.chain.
type ChainLink struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type FunctionConfig struct {
	Function   Function
	TenantID   string
	Chain      []ChainLink
	MaxRetries int
}

// ConfigStore loads config rows. Implementations: SQL (system DB) and in-memory for tests.
type ConfigStore interface {
	// LoadFunctionConfig returns the row for (fn, tenantID); tenantID "" means the global (NULL) row.
	// ok=false when no row exists.
	LoadFunctionConfig(ctx context.Context, fn Function, tenantID string) (cfg FunctionConfig, ok bool, err error)
	// EnabledProviders returns enabled provider ids. A nil map means "unknown/all enabled".
	EnabledProviders(ctx context.Context) (map[string]bool, error)
}

// DefaultTTL bounds how long a DB change takes to apply without restart (AC: ≤30s).
const DefaultTTL = 30 * time.Second

// CodeDefaults preserves legacy behaviour (Anthropic) when tables are empty.
func CodeDefaults(fn Function) FunctionConfig {
	switch fn {
	case FnEmbedding:
		return FunctionConfig{Function: fn, Chain: []ChainLink{{Provider: "openai", Model: "text-embedding-3-small"}}, MaxRetries: 1}
	case FnOCR, FnVision:
		return FunctionConfig{Function: fn, Chain: []ChainLink{{Provider: "anthropic", Model: "claude-haiku-4-5"}}, MaxRetries: 1}
	default:
		return FunctionConfig{Function: fn, Chain: []ChainLink{{Provider: "anthropic", Model: ""}}, MaxRetries: 1} // "" = legacy model routing
	}
}

type cacheKey struct {
	fn     Function
	tenant string
}
type cacheEntry struct {
	cfg FunctionConfig
	exp time.Time
}

// Resolver resolves (fn, tenant) → (fn, NULL) → code default, with TTL cache + explicit invalidation.
type Resolver struct {
	store ConfigStore
	ttl   time.Duration
	now   func() time.Time
	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

func NewResolver(store ConfigStore, ttl time.Duration) *Resolver {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Resolver{store: store, ttl: ttl, now: time.Now, cache: map[cacheKey]cacheEntry{}}
}

// Invalidate drops the cache (call from config-change Pub/Sub handler in BE-06).
func (r *Resolver) Invalidate() {
	r.mu.Lock()
	r.cache = map[cacheKey]cacheEntry{}
	r.mu.Unlock()
}

func (r *Resolver) Resolve(ctx context.Context, fn Function, tenantID string) (FunctionConfig, error) {
	k := cacheKey{fn, tenantID}
	r.mu.Lock()
	if e, ok := r.cache[k]; ok && r.now().Before(e.exp) {
		r.mu.Unlock()
		return e.cfg, nil
	}
	r.mu.Unlock()

	cfg, err := r.load(ctx, fn, tenantID)
	if err != nil {
		// DB failure must not break AI: fall back to code default (not cached).
		return CodeDefaults(fn), err
	}
	r.mu.Lock()
	r.cache[k] = cacheEntry{cfg: cfg, exp: r.now().Add(r.ttl)}
	r.mu.Unlock()
	return cfg, nil
}

func (r *Resolver) load(ctx context.Context, fn Function, tenantID string) (FunctionConfig, error) {
	if r.store == nil {
		return CodeDefaults(fn), nil
	}
	var (
		cfg   FunctionConfig
		found bool
		err   error
	)
	if tenantID != "" {
		cfg, found, err = r.store.LoadFunctionConfig(ctx, fn, tenantID)
		if err != nil {
			return FunctionConfig{}, err
		}
	}
	if !found {
		cfg, found, err = r.store.LoadFunctionConfig(ctx, fn, "")
		if err != nil {
			return FunctionConfig{}, err
		}
	}
	if !found {
		return CodeDefaults(fn), nil
	}
	enabled, err := r.store.EnabledProviders(ctx)
	if err != nil {
		return FunctionConfig{}, err
	}
	if enabled != nil {
		filtered := cfg.Chain[:0:0]
		for _, l := range cfg.Chain {
			if enabled[l.Provider] {
				filtered = append(filtered, l)
			}
		}
		cfg.Chain = filtered
	}
	if len(cfg.Chain) == 0 {
		return CodeDefaults(fn), nil
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	return cfg, nil
}

// ParseChain decodes the JSONB chain column.
func ParseChain(raw []byte) ([]ChainLink, error) {
	var out []ChainLink
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("llm: parse chain: %w", err)
	}
	return out, nil
}

// MemoryStore is an in-memory ConfigStore (tests / local dev).
type MemoryStore struct {
	mu        sync.Mutex
	Rows      map[string]FunctionConfig // key: fn + "|" + tenant
	Providers map[string]bool
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{Rows: map[string]FunctionConfig{}}
}

func (m *MemoryStore) Set(cfg FunctionConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Rows[string(cfg.Function)+"|"+cfg.TenantID] = cfg
}

func (m *MemoryStore) LoadFunctionConfig(_ context.Context, fn Function, tenantID string) (FunctionConfig, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.Rows[string(fn)+"|"+tenantID]
	return c, ok, nil
}

func (m *MemoryStore) EnabledProviders(context.Context) (map[string]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Providers == nil {
		return nil, nil
	}
	cp := map[string]bool{}
	for k, v := range m.Providers {
		cp[k] = v
	}
	return cp, nil
}
