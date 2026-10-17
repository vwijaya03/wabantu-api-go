package buyerflow

import (
	"strings"
	"testing"
)

func TestBuildCatalogContextIncludesDescription(t *testing.T) {
	cat := []CatalogItem{{ID: "tuna-100", ExternalCode: "YELLOW_TAIL_TUNA_PORTION_100_GRAM", Name: "Yellow Tail Tuna Portion 100 Gram", SellPrice: 350000, SellUnit: "pcs",
		Description: "Premium cut of frozen yellow tail tuna steak, 100 grams per portion. Sold frozen and vacuum-sealed to lock in freshness and premium quality. " + strings.Repeat("x", 300)}}
	ctx := BuildCatalogContext(cat)
	if !strings.Contains(ctx, "Sold frozen and vacuum-sealed") {
		t.Fatalf("description missing from LLM catalog context:\n%s", ctx)
	}
	if strings.Contains(ctx, strings.Repeat("x", 200)) {
		t.Fatal("description must be truncated (≤160 runes)")
	}
}
