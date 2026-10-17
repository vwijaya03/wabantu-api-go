package flag

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// uniqueFlagKey avoids clashes with rows left behind by an interrupted earlier run.
func uniqueFlagKey(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

// forgetFlag drops the cache entry and any DB row for key now and again when the test ends.
func forgetFlag(t *testing.T, key string) {
	t.Helper()
	clean := func() {
		invalidateCache(key)
		_, _ = db.Exec(context.Background(), `DELETE FROM feature_flag WHERE key=$1`, key)
	}
	clean()
	t.Cleanup(clean)
}

// An unknown key is cached as a disabled flag, so the second call must not query the DB. That is
// proved by inserting the row behind the cache's back: the cache keeps saying "disabled" until it
// is invalidated, and only then does the real row show up.
func TestNegativeCacheUnknownFlag(t *testing.T) {
	ctx := context.Background()
	key := uniqueFlagKey("test_negcache")
	forgetFlag(t, key)

	if IsEnabled(ctx, key, "tenant-a") {
		t.Fatal("an unknown flag must be disabled")
	}
	f, ok := getCached(key)
	if !ok {
		t.Fatal("a miss must be cached")
	}
	if f.Key != key || f.EnabledGlobally || len(f.TenantIDs) != 0 {
		t.Fatalf("a cached miss must be a disabled flag, got %+v", *f)
	}

	if _, err := db.Exec(ctx, `INSERT INTO feature_flag (key, enabled_globally) VALUES ($1, true)`, key); err != nil {
		t.Fatalf("insert flag: %v", err)
	}
	if IsEnabled(ctx, key, "tenant-a") {
		t.Fatal("the second call must be answered by the negative cache without querying the DB")
	}

	invalidateCache(key) // what addTenantToFlag and removeTenantFromFlag do after a write
	if !IsEnabled(ctx, key, "tenant-a") {
		t.Fatal("once the cache entry is gone the real row must be read")
	}
}

// CreateFlag and UpdateFlag call setCache with the stored row; that must replace a cached miss.
func TestNegativeCacheReplacedBySetCache(t *testing.T) {
	ctx := context.Background()
	key := uniqueFlagKey("test_negcache_set")
	forgetFlag(t, key)

	if IsEnabled(ctx, key, "tenant-a") {
		t.Fatal("an unknown flag must be disabled")
	}
	setCache(FeatureFlag{Key: key, TenantIDs: []string{"tenant-a"}})
	if !IsEnabled(ctx, key, "tenant-a") || IsEnabled(ctx, key, "tenant-b") {
		t.Fatal("the stored flag must replace the cached miss")
	}
}

// Only "no such flag" is negative-cached. Any other lookup error reads as disabled for this call
// but must be retried by the next one, otherwise a DB blip would switch a flag off for 60 s.
func TestNegativeCacheIgnoresDBErrors(t *testing.T) {
	key := uniqueFlagKey("test_negcache_err")
	forgetFlag(t, key)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // every query on this context fails with a context error, not ErrNoRows
	if IsEnabled(ctx, key, "tenant-a") {
		t.Fatal("a failed lookup must read as disabled")
	}
	if _, ok := getCached(key); ok {
		t.Fatal("a DB error must not be cached")
	}
}

func TestLLMFirstFlagKeys(t *testing.T) {
	// These strings are what operators type into POST /api/v1/flags, so they must not drift.
	if FlagLLMFirst != "ai_llm_first" || FlagLLMFirstShadow != "ai_llm_first_shadow" || FlagLLMFirstKill != "ai_llm_first_kill" {
		t.Fatalf("flag keys changed: %q %q %q", FlagLLMFirst, FlagLLMFirstShadow, FlagLLMFirstKill)
	}
}

// forgetLLMFirstFlags clears cache entries and DB rows of the three LLM-first flags now and again
// when the test ends.
func forgetLLMFirstFlags(t *testing.T) {
	t.Helper()
	for _, key := range []string{FlagLLMFirst, FlagLLMFirstShadow, FlagLLMFirstKill} {
		forgetFlag(t, key)
	}
}

// With no rows at all (the state of every tenant before rollout) the mode is off, and the three
// missing flags are negative-cached so the next message costs no queries.
func TestLLMFirstModeOffWithoutRows(t *testing.T) {
	forgetLLMFirstFlags(t)

	if got := LLMFirstMode(context.Background(), "tenant-a"); got != "off" {
		t.Fatalf("no rows must mean off, got %q", got)
	}
	for _, key := range []string{FlagLLMFirst, FlagLLMFirstShadow, FlagLLMFirstKill} {
		if _, ok := getCached(key); !ok {
			t.Errorf("missing flag %s must be negative-cached", key)
		}
	}
}

// Order of checks: kill (global) > ai_llm_first for the tenant > ai_llm_first_shadow for the
// tenant > off. The flags are seeded through the cache, which is what IsEnabled reads first.
func TestLLMFirstModeOrderOfChecks(t *testing.T) {
	const tenant, other = "tenant-a", "tenant-b"
	forgetLLMFirstFlags(t)

	only := func(ids ...string) FeatureFlag { return FeatureFlag{TenantIDs: ids} }
	global := FeatureFlag{EnabledGlobally: true}
	none := FeatureFlag{}
	cases := []struct {
		name             string
		kill, on, shadow FeatureFlag
		want             string
	}{
		{"nothing enabled", none, none, none, "off"},
		{"on for the tenant", none, only(tenant), none, "on"},
		{"on for another tenant only", none, only(other), none, "off"},
		{"on globally", none, global, none, "on"},
		{"shadow for the tenant", none, none, only(tenant), "shadow"},
		{"shadow for another tenant only", none, none, only(other), "off"},
		{"shadow globally", none, none, global, "shadow"},
		{"on wins over shadow", none, only(tenant), only(tenant), "on"},
		{"kill wins over on", global, only(tenant), none, "off"},
		{"kill wins over a global on", global, global, none, "off"},
		{"kill wins over shadow", global, none, only(tenant), "off"},
		{"kill wins over everything", global, global, global, "off"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.kill.Key, c.on.Key, c.shadow.Key = FlagLLMFirstKill, FlagLLMFirst, FlagLLMFirstShadow
			setCache(c.kill)
			setCache(c.on)
			setCache(c.shadow)
			if got := LLMFirstMode(context.Background(), tenant); got != c.want {
				t.Fatalf("mode = %q, want %q", got, c.want)
			}
		})
	}
}
