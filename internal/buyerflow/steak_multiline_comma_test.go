package buyerflow

import (
	"strings"
	"testing"
)

func TestMultilineCommaTwoSKUs_inlineSplit(t *testing.T) {
	msg := "Ehmm okay\nI wang to order 1 salmons teak 100gr, 1 Yellow tail tuna , please"
	if !IsInlineMultiOrderMessage(msg) {
		t.Fatal("expected inline multi order message")
	}
	lines := ParseInlineMultiOrderLines(msg, omahSteakCatalog())
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	ids := map[string]bool{}
	for _, ln := range lines {
		if ln.Qty != 1 {
			t.Fatalf("qty want 1, got %d for %s", ln.Qty, ln.ProductName)
		}
		ids[ln.CatalogItemID] = true
	}
	if !ids["salmon-100"] || !ids["tuna-100"] {
		t.Fatalf("want salmon-100 and tuna-100, got %+v", lines)
	}
}

func TestSteakMultilineCommaTwoSKUs_simulator(t *testing.T) {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	msg := "Ehmm okay\nI wang to order 1 salmons teak 100gr, 1 Yellow tail tuna , please"
	out := s.Turn(msg)
	if out.Order == nil || !out.Order.HasMultiItems() || len(out.Order.Items) != 2 {
		t.Fatalf("expected 2-item cart, order=%+v path=%s", out.Order, out.Path)
	}
	summary := strings.ToLower(out.Reply)
	if !strings.Contains(summary, "yellow tail") || !strings.Contains(summary, "salmon") {
		t.Fatalf("summary missing SKU names:\n%s", out.Reply)
	}
}
