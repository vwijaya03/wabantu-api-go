package ai

import "testing"

func TestNormalizeCatalogLoadLimit(t *testing.T) {
	if got := normalizeCatalogLoadLimit(200); got != 200 {
		t.Fatalf("200 must stay 200, got %d (must not clamp to 40)", got)
	}
	if got := normalizeCatalogLoadLimit(0); got != defaultCatalogLoadLimit {
		t.Fatalf("invalid limit must default to %d, got %d", defaultCatalogLoadLimit, got)
	}
	if got := normalizeCatalogLoadLimit(9999); got != maxCatalogLoadLimit {
		t.Fatalf("oversized limit must cap at %d, got %d", maxCatalogLoadLimit, got)
	}
	if defaultCatalogLoadLimit < 200 {
		t.Fatalf("default catalog window %d is too small; SKUs after LIMIT were silently dropped", defaultCatalogLoadLimit)
	}
}
