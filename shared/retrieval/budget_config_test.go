package retrieval

import (
	"testing"
	"time"
)

func TestQueryBudgetDefaults(t *testing.T) {
	// parseBudgetOverride is safe under plain go test; full env defaults need encore test runtime.
	budget := parseBudgetOverride("1200")
	if budget != 1200*time.Millisecond {
		t.Fatalf("expected 1200ms override parse, got %v", budget)
	}
}

func TestParseBudgetOverrideClamp(t *testing.T) {
	if parseBudgetOverride("50") != 200*time.Millisecond {
		t.Fatal("below min should clamp to 200ms")
	}
	if parseBudgetOverride("99999") != 10*time.Second {
		t.Fatal("above max should clamp to 10s")
	}
	if parseBudgetOverride("1500") != 1500*time.Millisecond {
		t.Fatal("valid override should apply")
	}
}

func TestQueryBudgetUsesSecretOverride(t *testing.T) {
	orig := secrets.RetrievalBudgetMs
	defer func() { secrets.RetrievalBudgetMs = orig }()
	secrets.RetrievalBudgetMs = "1800"
	if QueryBudget() != 1800*time.Millisecond {
		t.Fatalf("override not applied: %v", QueryBudget())
	}
}
