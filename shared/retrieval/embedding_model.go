package retrieval

import (
	"context"
	"strings"
	"sync"

	"encore.app/wabantu/shared/llm"
)

// EmbeddingModelResolver returns the embedding model id active for a tenant.
type EmbeddingModelResolver func(ctx context.Context, tenantID string) (string, error)

var (
	modelResolverMu sync.Mutex
	modelResolver   EmbeddingModelResolver

	lastKnownMu    sync.RWMutex
	lastKnownModel = map[string]string{}
)

// SetEmbeddingModelResolver overrides how the active embedding model is resolved (tests, local dev).
// Passing nil restores the default (system DB ai_function_config via llm.Resolver).
func SetEmbeddingModelResolver(fn EmbeddingModelResolver) {
	modelResolverMu.Lock()
	modelResolver = fn
	modelResolverMu.Unlock()
}

// SetSystemDBProvider wires the system DB (ai_function_config) for the default resolver. Infrastructure
// resources cannot be declared in a library package, so a service (kb) passes a lazy accessor.
// Without it (tests, local tools) the active model is the OpenAI default.
func SetSystemDBProvider(p func() llm.Querier) {
	modelResolverMu.Lock()
	systemDBProvider = p
	defaultResolver = nil
	modelResolverMu.Unlock()
}

var (
	systemDBProvider func() llm.Querier
	defaultResolver  EmbeddingModelResolver
)

func currentResolver() EmbeddingModelResolver {
	modelResolverMu.Lock()
	defer modelResolverMu.Unlock()
	if modelResolver != nil {
		return modelResolver
	}
	if defaultResolver != nil {
		return defaultResolver
	}
	if systemDBProvider == nil {
		return func(context.Context, string) (string, error) { return EmbeddingModel, nil }
	}
	store := llm.NewSQLStore(systemDBProvider())
	r := llm.NewResolver(store, llm.DefaultTTL)
	llm.OnConfigChanged(r.Invalidate)
	defaultResolver = func(ctx context.Context, tenantID string) (string, error) {
		cfg, err := r.Resolve(ctx, llm.FnEmbedding, tenantID)
		if err != nil {
			return "", err
		}
		return EmbeddingModelFromChain(cfg.Chain), nil
	}
	return defaultResolver
}

// EmbeddingModelFromChain picks the single embedding link (chain is max 1 link). Unknown values fall back to OpenAI.
func EmbeddingModelFromChain(chain []llm.ChainLink) string {
	if len(chain) == 0 {
		return EmbeddingModel
	}
	l := chain[0]
	m := strings.TrimSpace(l.Model)
	if m == "" && l.Provider == "openai" {
		return EmbeddingModel
	}
	if llm.IsEmbeddingModel(m) {
		return m
	}
	return EmbeddingModel
}

// ResolveEmbeddingModel returns the embedding model for tenantID (per-tenant override → global → OpenAI default).
// An error means the config could not be read: callers on write/query paths must not guess a model.
func ResolveEmbeddingModel(ctx context.Context, tenantID string) (string, error) {
	m, err := currentResolver()(ctx, tenantID)
	if err != nil {
		return "", err
	}
	if !llm.IsEmbeddingModel(m) {
		m = EmbeddingModel
	}
	lastKnownMu.Lock()
	lastKnownModel[tenantID] = m
	lastKnownMu.Unlock()
	return m, nil
}

// ActiveEmbeddingModel is the non-failing variant used when stamping PG rows: on resolver error it
// returns the last model seen for the tenant, else the OpenAI default. The index worker re-resolves
// strictly (ResolveEmbeddingModel) and rejects rows whose stamped model differs.
func ActiveEmbeddingModel(ctx context.Context, tenantID string) string {
	if m, err := ResolveEmbeddingModel(ctx, tenantID); err == nil {
		return m
	}
	lastKnownMu.RLock()
	m, ok := lastKnownModel[tenantID]
	lastKnownMu.RUnlock()
	if ok {
		return m
	}
	return EmbeddingModel
}

// KnownEmbeddingModels lists every model whose vectors may exist in a namespace (for deletes).
func KnownEmbeddingModels() []string {
	return []string{llm.OpenAIEmbeddingModel, llm.GeminiEmbeddingModel}
}
