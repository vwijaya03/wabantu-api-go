package codesim

import "testing"

func TestFilterExcludedMCQs_FallbackWhenEmpty(t *testing.T) {
	pool := []MCQItem{{ID: "a"}, {ID: "b"}}
	exclude := map[string]bool{"a": true, "b": true}
	got := filterExcludedMCQs(pool, exclude)
	if len(got) != 2 {
		t.Fatalf("expected fallback to full pool, got %d", len(got))
	}
}

func TestFilterExcludedMCQs_ExcludesUsed(t *testing.T) {
	pool := []MCQItem{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	exclude := map[string]bool{"a": true}
	got := filterExcludedMCQs(pool, exclude)
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "c" {
		t.Fatalf("unexpected filtered pool: %+v", got)
	}
}

func TestFilterExcludedBuilds_ExcludesUsed(t *testing.T) {
	pool := []BuildTask{{ID: "x"}, {ID: "y"}}
	exclude := map[string]bool{"x": true}
	got := filterExcludedBuilds(pool, exclude)
	if len(got) != 1 || got[0].ID != "y" {
		t.Fatalf("unexpected filtered builds: %+v", got)
	}
}

func TestFilterExcludedDebugs_NoExcludeReturnsPool(t *testing.T) {
	pool := []DebugTask{{ID: "d1"}}
	got := filterExcludedDebugs(pool, nil)
	if len(got) != 1 {
		t.Fatalf("expected unchanged pool")
	}
}
