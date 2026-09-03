package buyerflow

import (
	"strings"
	"testing"
)

func omahFoodCatalog() []CatalogItem {
	return []CatalogItem{
		{ID: "maggi-percik", ExternalCode: "MAGGI_BAG_AYAM_PERCIK", Name: "Maggi Bumbu Ayam Goreng - Ayam Percik", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "abon-250", Name: "Abon Sapi 250 Gram", SellPrice: 20000, SellUnit: "pcs"},
		{ID: "nutella", Name: "Nutella Biskuit (193g)", SellPrice: 155000, SellUnit: "pcs"},
	}
}

func TestIsInlineMultiOrderMessage(t *testing.T) {
	msg := "1 pcs, lalu abon sapi yang 250 gram 1pcs"
	if !IsInlineMultiOrderMessage(msg) {
		t.Fatal("expected inline multi")
	}
	if !IsStructuredOrderList(msg) {
		t.Fatal("expected structured order list")
	}
}

func TestParseInlineMultiOrderLines(t *testing.T) {
	catalog := omahFoodCatalog()
	msg := "maggi percik 1 pcs, lalu abon sapi 250 gram 1 pcs"
	lines := ParseInlineMultiOrderLines(msg, catalog)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	if lines[0].CatalogItemID != "maggi-percik" {
		t.Fatalf("line0 want maggi, got %q", lines[0].ProductName)
	}
	if lines[1].CatalogItemID != "abon-250" {
		t.Fatalf("line1 want abon 250, got %q", lines[1].ProductName)
	}
}

func TestTryAppendItemsDuringCheckout(t *testing.T) {
	catalog := omahFoodCatalog()
	st := OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "maggi-percik",
		ProductName:   "Maggi Bumbu Ayam Goreng - Ayam Percik",
		Qty:           1,
		UnitPrice:     70000,
		SellUnit:      "pcs",
	}
	tmpl := orderTemplatesFromKB(nil, false)
	handled, reply := TryAppendItemsDuringCheckout(&st, "1 pcs, lalu abon sapi yang 250 gram 1pcs", catalog, tmpl, false, nil)
	if !handled {
		t.Fatal("expected handled append")
	}
	if len(st.Items) != 2 {
		t.Fatalf("want 2 items, got %d", len(st.Items))
	}
	if st.Items[1].CatalogItemID != "abon-250" {
		t.Fatalf("want abon line, got %+v", st.Items[1])
	}
	if !strings.Contains(reply, "Abon") {
		t.Fatalf("reply should mention abon: %s", reply)
	}
}

func TestTryAppendSingleLaluItem(t *testing.T) {
	catalog := omahFoodCatalog()
	st := OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "maggi-percik",
		ProductName:   "Maggi Bumbu Ayam Goreng - Ayam Percik",
		Qty:           1,
		UnitPrice:     70000,
		SellUnit:      "pcs",
	}
	tmpl := orderTemplatesFromKB(nil, false)
	handled, _ := TryAppendItemsDuringCheckout(&st, "lalu nutela biskuit 1 piece", catalog, tmpl, false, nil)
	if !handled {
		t.Fatal("expected handled")
	}
	if len(st.Items) != 2 {
		t.Fatalf("want 2 items, got %d items=%+v", len(st.Items), st.Items)
	}
	if st.Items[1].CatalogItemID != "nutella" {
		t.Fatalf("want nutella, got %+v", st.Items[1])
	}
}
