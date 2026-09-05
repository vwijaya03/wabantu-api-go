package buyerflow

import (
	"strings"
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

func TestMatchCatalogItemSemanticSortsUnorderedHits(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "1", Name: "Kaos Pria L", ExternalCode: "KP-L"},
		{ID: "2", Name: "Kaos Pria M", ExternalCode: "KP-M"},
	}
	hits := []retrieval.Hit{
		{ID: "c2", Score: 0.70, Metadata: map[string]any{"entry_id": "2"}},
		{ID: "c1", Score: 0.90, Metadata: map[string]any{"entry_id": "1"}},
	}
	if m := MatchCatalogItemSemantic("kaos pria L", catalog, hits); m == nil || m.ID != "1" {
		t.Fatalf("expected clear winner after sort, got %+v", m)
	}
}

func TestMatchCatalogItemSemanticSingleHitLowScoreNoAutoPick(t *testing.T) {
	catalog := []CatalogItem{{ID: "1", Name: "Abon Sapi 500G", SellPrice: 35000}}
	hits := []retrieval.Hit{{Score: 0.30, Metadata: map[string]any{"entry_id": "1"}}}
	if m := MatchCatalogItemSemantic("halo kak", catalog, hits); m != nil {
		t.Fatalf("low-score single hit must not auto-pick: %+v", m)
	}
}

func TestMatchCatalogItemSemanticDropsRRFScaleHitsThenLexicalFallback(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "zebra", Name: "Zebra Cake", SellPrice: 10000},
		{ID: "nutella", Name: "Nutella Biskuit", SellPrice: 155000},
	}
	// RRF k=60 rank-1 ≈ 0.016 — not a cosine quality gate. Must not narrow the catalog.
	hits := []retrieval.Hit{{Score: 0.016, Metadata: map[string]any{"entry_id": "zebra"}}}
	m := MatchCatalogItemSemantic("biskuit", catalog, hits)
	if m == nil || m.ID != "nutella" {
		t.Fatalf("RRF-scale hits must be dropped; lexical should find nutella, got %+v", m)
	}
}

func TestCatalogSemanticAmbiguousSortsHits(t *testing.T) {
	hits := []retrieval.Hit{
		{Score: 0.885},
		{Score: 0.90},
	}
	if !CatalogSemanticAmbiguous(hits) {
		t.Fatal("expected ambiguous after sorting top two scores")
	}
}

func TestReplyFromBusinessCatalogListBypassesAmbiguity(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "1", Name: "Abon Sapi 125g", SellPrice: 15000, SellUnit: "pcs"},
		{ID: "2", Name: "Maggi Ayam Berempah", SellPrice: 70000, SellUnit: "pcs"},
	}
	hits := []retrieval.Hit{
		{Score: 0.88, Metadata: map[string]any{"entry_id": "2"}},
		{Score: 0.90, Metadata: map[string]any{"entry_id": "1"}},
	}
	vctx := &CatalogVectorContext{Hits: hits}
	profile := &BusinessProfile{BusinessName: "Toko", Tone: strPtr("casual")}
	reply, ok := replyFromBusinessCatalog("minta list produk", profile, catalog, nil, vctx)
	if !ok {
		t.Fatal("expected handled list reply")
	}
	if strings.Contains(reply, "sebutin produk/SKU") {
		t.Fatalf("list question must not get ambiguity reply: %q", reply)
	}
	if !strings.Contains(reply, "katalog") {
		t.Fatalf("expected catalog list intro: %q", reply)
	}
}

func TestReplyFromBusinessCatalogProductInquiryAmbiguous(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "1", Name: "Kaos Pria L", ExternalCode: "KP-L", SellPrice: 100000},
		{ID: "2", Name: "Kaos Pria M", ExternalCode: "KP-M", SellPrice: 100000},
	}
	hits := []retrieval.Hit{
		{Score: 0.90, Metadata: map[string]any{"entry_id": "1"}},
		{Score: 0.88, Metadata: map[string]any{"entry_id": "2"}},
	}
	vctx := &CatalogVectorContext{Hits: hits}
	profile := &BusinessProfile{BusinessName: "Toko", Tone: strPtr("casual")}
	reply, ok := replyFromBusinessCatalog("berapa harganya kak?", profile, catalog, nil, vctx)
	if !ok {
		t.Fatal("expected handled reply")
	}
	if !strings.Contains(reply, "sebutin produk/SKU") {
		t.Fatalf("expected ambiguity clarification, got: %q", reply)
	}
}
