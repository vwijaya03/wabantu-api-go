package buyerflow

import (
	"strings"
	"testing"

	"encore.app/wabantu/shared/retrieval"
)

func TestResolveOrderProductMatch_lexicalWinsOverVector(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "cad1", Name: "Cadbury biscoff bar 130 gram", SellPrice: 105000},
		{ID: "mag1", Name: "Maggi Bumbu Ayam Goreng - Ayam Berempah", SellPrice: 70000},
	}
	vctx := &CatalogVectorContext{Hits: []retrieval.Hit{
		{Score: 0.9, Metadata: map[string]any{"entry_id": "mag1"}},
	}}
	m := resolveOrderProductMatch("cadbury mau pesen 1 pcs", nil, catalog, vctx)
	if m == nil || m.ID != "cad1" {
		t.Fatalf("lexical should win, got %v", m)
	}
}

func TestResolveOrderProductMatch_vectorAfterLexicalMiss(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "cad1", Name: "Cadbury biscoff bar 130 gram", SellPrice: 105000},
	}
	vctx := &CatalogVectorContext{Hits: []retrieval.Hit{
		{Score: 0.62, Metadata: map[string]any{"entry_id": "cad1"}},
	}}
	m := resolveOrderProductMatch("xyz unknown product", nil, catalog, vctx)
	if m == nil || m.ID != "cad1" {
		t.Fatalf("expected semantic auto-pick, got %v", m)
	}
}

func TestResolveOrderProductMatch_ambiguousCadbury(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "cad1", Name: "Cadbury biscoff bar 130 gram", SellPrice: 105000},
		{ID: "cad2", Name: "Cadbury biscoff mini bars", SellPrice: 110000},
	}
	vctx := &CatalogVectorContext{Hits: []retrieval.Hit{
		{Score: 0.50, Metadata: map[string]any{"entry_id": "cad1"}},
		{Score: 0.48, Metadata: map[string]any{"entry_id": "cad2"}},
	}}
	m := resolveOrderProductMatch("xyz unknown", nil, catalog, vctx)
	if m != nil {
		t.Fatalf("ambiguous vector hits should not auto-pick, got %v", m)
	}
}

func TestExtractProductQueryForEmbed(t *testing.T) {
	got := ExtractProductQueryForEmbed("cadburi mau pesen 1 pcs")
	if got != "cadburi" {
		t.Fatalf("want cadburi, got %q", got)
	}
}

func TestOrderFSM_vectorTypoCheckout(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "cad1", Name: "Cadbury biscoff bar 130 gram", SellPrice: 105000},
		{ID: "mag1", Name: "Maggi Bumbu Ayam Goreng - Ayam Berempah", SellPrice: 70000},
	}
	body := "Ini katalog Omah Apparel ya kak:\n\n• Cadbury biscoff bar 130 gram\n• Maggi Bumbu Ayam Goreng - Ayam Berempah"
	history := []Message{{Direction: "out", Body: body}}
	vctx := &CatalogVectorContext{Hits: []retrieval.Hit{
		{Score: 0.62, Metadata: map[string]any{"entry_id": "cad1"}},
	}}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText:  "cadburi mau pesen 1 pcs",
		Catalog:   catalog,
		History:   history,
		VectorCtx: vctx,
	}, func(st OrderState) (string, error) { return "", nil })
	if res.State == nil {
		t.Fatalf("expected order state, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.State.ProductName), "cadbury") {
		t.Fatalf("expected cadbury, got %q", res.State.ProductName)
	}
}

func TestOrderFSM_vectorAmbiguousAskVariant(t *testing.T) {
	catalog := []CatalogItem{
		{ID: "cad1", Name: "Cadbury biscoff bar 130 gram", SellPrice: 105000},
		{ID: "cad2", Name: "Cadbury biscoff mini bars", SellPrice: 110000},
		{ID: "mag1", Name: "Maggi Bumbu Ayam Goreng - Ayam Berempah", SellPrice: 70000},
	}
	body := "Ini katalog Omah Apparel ya kak:\n\n• Maggi Bumbu Ayam Goreng - Ayam Berempah"
	history := []Message{{Direction: "out", Body: body}}
	vctx := &CatalogVectorContext{Hits: []retrieval.Hit{
		{Score: 0.50, Metadata: map[string]any{"entry_id": "cad1"}},
		{Score: 0.48, Metadata: map[string]any{"entry_id": "cad2"}},
	}}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText:  "mau pesen 1 pcs",
		Catalog:   catalog,
		History:   history,
		VectorCtx: vctx,
	}, func(st OrderState) (string, error) { return "", nil })
	if res.State == nil || res.State.Step != "ask_variant" {
		t.Fatalf("expected ask_variant, got state=%+v reply=%q", res.State, res.Reply)
	}
	if res.State.ProductName != "" && strings.Contains(strings.ToLower(res.State.ProductName), "maggi") {
		t.Fatalf("history should not hijack to maggi: %q", res.State.ProductName)
	}
	if !strings.Contains(strings.ToLower(res.Reply), "cadbury") {
		t.Fatalf("expected cadbury variant picker, got %q", res.Reply)
	}
}
