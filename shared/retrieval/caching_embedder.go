package retrieval

import (
	"context"
	"fmt"
)

// CachingEmbedder wraps an Embedder and caches single-text embeddings (query hot path).
type CachingEmbedder struct {
	inner Embedder
	cache *queryEmbedCache
}

// NewCachingEmbedder wraps base with an LRU query cache. Batch embeds (indexing) bypass cache.
func NewCachingEmbedder(inner Embedder) *CachingEmbedder {
	if inner == nil {
		return nil
	}
	return &CachingEmbedder{
		inner: inner,
		cache: newQueryEmbedCache(defaultQueryCacheSize, defaultQueryCacheTTL),
	}
}

func (c *CachingEmbedder) Dimensions() int {
	if c == nil || c.inner == nil {
		return 0
	}
	return c.inner.Dimensions()
}

func (c *CachingEmbedder) Model() string {
	if c == nil || c.inner == nil {
		return ""
	}
	return c.inner.Model()
}

func (c *CachingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if c == nil || c.inner == nil {
		return nil, fmt.Errorf("caching embedder not configured")
	}
	if len(texts) == 1 {
		if vec, ok := c.cache.get(c.inner.Model(), texts[0]); ok {
			return [][]float32{vec}, nil
		}
	}
	out, err := c.inner.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(texts) == 1 && len(out) == 1 {
		c.cache.put(c.inner.Model(), texts[0], out[0])
	}
	return out, nil
}

// CacheStats returns hits, misses, and current cache size.
func (c *CachingEmbedder) CacheStats() (hits, misses uint64, size int) {
	if c == nil || c.cache == nil {
		return 0, 0, 0
	}
	return c.cache.stats()
}
