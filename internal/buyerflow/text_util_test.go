package buyerflow

import (
	"strings"
	"testing"
)

func TestTokenizePreservesApparelSizes(t *testing.T) {
	t.Parallel()
	l := tokenize("[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L")
	m := tokenize("[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - M")
	if overlapScore(l, m) >= 1.0 {
		t.Fatalf("L and M token sets must not be identical: L=%v M=%v", l, m)
	}
	if !containsToken(l, "l") {
		t.Fatalf("expected size token l in %v", l)
	}
	if !containsToken(m, "m") {
		t.Fatalf("expected size token m in %v", m)
	}
}

func TestTokenizeQuerySizeDifferentiatesCatalogMatch(t *testing.T) {
	t.Parallel()
	catalog := omahCatalog()
	lMatch := matchCatalogItem("boxer mono spot ukuran L berapa?", catalog)
	mMatch := matchCatalogItem("boxer mono spot ukuran M berapa?", catalog)
	if lMatch == nil || mMatch == nil {
		t.Fatal("expected both L and M queries to match catalog items")
	}
	if lMatch.ID == mMatch.ID {
		t.Fatalf("L query matched %q, M query matched same item %q", lMatch.ID, mMatch.ID)
	}
	if !strings.Contains(lMatch.Name, "- L") {
		t.Fatalf("L query should match L SKU, got %q", lMatch.Name)
	}
	if !strings.Contains(mMatch.Name, "- M") {
		t.Fatalf("M query should match M SKU, got %q", mMatch.Name)
	}
}

func TestNormalizeQuestionDiffersBySize(t *testing.T) {
	t.Parallel()
	a := strings.Join(tokenize("boxer mono spot ukuran L"), " ")
	b := strings.Join(tokenize("boxer mono spot ukuran M"), " ")
	if a == b {
		t.Fatalf("normalized questions must differ: %q vs %q", a, b)
	}
}

func containsToken(tokens []string, want string) bool {
	for _, tok := range tokens {
		if tok == want {
			return true
		}
	}
	return false
}
