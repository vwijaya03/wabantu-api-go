package retrieval

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

const (
	defaultQueryCacheSize = 512
	defaultQueryCacheTTL  = 15 * time.Minute
)

type cacheEntry struct {
	vector    []float32
	expiresAt time.Time
}

// queryEmbedCache is an in-process LRU cache for single-query embeddings.
type queryEmbedCache struct {
	mu      sync.Mutex
	max     int
	ttl     time.Duration
	items   map[string]cacheEntry
	order   []string
	hits    uint64
	misses  uint64
}

func newQueryEmbedCache(max int, ttl time.Duration) *queryEmbedCache {
	if max <= 0 {
		max = defaultQueryCacheSize
	}
	if ttl <= 0 {
		ttl = defaultQueryCacheTTL
	}
	return &queryEmbedCache{
		max:   max,
		ttl:   ttl,
		items: make(map[string]cacheEntry, max),
		order: make([]string, 0, max),
	}
}

func queryCacheKey(model, text string) string {
	norm := strings.TrimSpace(strings.ToLower(text))
	h := sha256.Sum256([]byte(model + "\x00" + norm))
	return hex.EncodeToString(h[:])
}

func (c *queryEmbedCache) get(model, text string) ([]float32, bool) {
	if c == nil {
		return nil, false
	}
	key := queryCacheKey(model, text)
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.items[key]
	if !ok || now.After(entry.expiresAt) {
		if ok {
			delete(c.items, key)
			c.removeKeyFromOrder(key)
		}
		c.misses++
		return nil, false
	}
	c.touchLocked(key)
	c.hits++
	out := make([]float32, len(entry.vector))
	copy(out, entry.vector)
	return out, true
}

func (c *queryEmbedCache) put(model, text string, vector []float32) {
	if c == nil || len(vector) == 0 {
		return
	}
	key := queryCacheKey(model, text)
	cp := make([]float32, len(vector))
	copy(cp, vector)

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.items[key]; !ok && len(c.items) >= c.max {
		c.evictOldestLocked()
	}
	c.items[key] = cacheEntry{vector: cp, expiresAt: time.Now().Add(c.ttl)}
	c.touchLocked(key)
}

func (c *queryEmbedCache) stats() (hits, misses uint64, size int) {
	if c == nil {
		return 0, 0, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses, len(c.items)
}

func (c *queryEmbedCache) touchLocked(key string) {
	c.removeKeyFromOrder(key)
	c.order = append(c.order, key)
}

func (c *queryEmbedCache) removeKeyFromOrder(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

func (c *queryEmbedCache) evictOldestLocked() {
	for len(c.order) > 0 && len(c.items) >= c.max {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.items, oldest)
	}
}
