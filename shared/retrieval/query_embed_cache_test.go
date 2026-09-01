package retrieval

import (
	"context"
	"testing"
)

func TestQueryEmbedCacheHitMiss(t *testing.T) {
	c := newQueryEmbedCache(4, defaultQueryCacheTTL)
	vec := []float32{0.1, 0.2, 0.3}
	c.put("m", "hello", vec)

	got, ok := c.get("m", "hello")
	if !ok || len(got) != 3 {
		t.Fatalf("cache miss on hit")
	}
	got[0] = 9
	got2, ok := c.get("m", "hello")
	if !ok || got2[0] != 0.1 {
		t.Fatal("cache should return copy")
	}

	_, ok = c.get("m", "other")
	if ok {
		t.Fatal("expected miss")
	}
	hits, misses, size := c.stats()
	if hits != 2 || misses != 1 || size != 1 {
		t.Fatalf("stats hits=%d misses=%d size=%d", hits, misses, size)
	}
}

func TestCachingEmbedderSingleText(t *testing.T) {
	mock := NewMockEmbedder()
	cached := NewCachingEmbedder(mock)
	ctx := context.Background()

	out1, err := cached.Embed(ctx, []string{"query one"})
	if err != nil || len(out1) != 1 {
		t.Fatal(err)
	}
	out2, err := cached.Embed(ctx, []string{"query one"})
	if err != nil || len(out2) != 1 {
		t.Fatal(err)
	}
	hits, misses, _ := cached.CacheStats()
	if hits != 1 || misses != 1 {
		t.Fatalf("hits=%d misses=%d", hits, misses)
	}

	_, err = cached.Embed(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
}
