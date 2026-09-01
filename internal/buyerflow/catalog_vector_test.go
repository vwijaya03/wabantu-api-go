package buyerflow

import (
	"testing"

	"encore.app/wabantu/shared/retrieval"
)

func TestMatchCatalogItemSemanticAmbiguous(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "1", Name: "Kaos Pria L", ExternalCode: "KP-L"},
		{ID: "2", Name: "Kaos Pria M", ExternalCode: "KP-M"},
	}
	hits := []retrieval.Hit{
		{ID: "c1", Score: 0.9, Metadata: map[string]any{"entry_id": "1"}},
		{ID: "c2", Score: 0.88, Metadata: map[string]any{"entry_id": "2"}},
	}
	if m := MatchCatalogItemSemantic("kaos pria", catalog, hits); m != nil {
		t.Fatalf("expected ambiguity nil, got %+v", m)
	}
}

func TestMatchCatalogItemSemanticSingleHit(t *testing.T) {
	catalog := []CatalogItem{{ID: "1", Name: "Kaos Pria L", ExternalCode: "KP-L"}}
	hits := []retrieval.Hit{{ID: "c1", Score: 0.9, Metadata: map[string]any{"entry_id": "1"}}}
	m := MatchCatalogItemSemantic("kaos pria L", catalog, hits)
	if m == nil || m.ID != "1" {
		t.Fatalf("expected match, got %+v", m)
	}
}
