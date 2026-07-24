package inventory

import "testing"

func TestIsCommittedOrderStatus(t *testing.T) {
	committed := []string{"processing", "shipped", "completed", "paid", "confirmed", "PROCESSING", " Completed "}
	for _, s := range committed {
		if !IsCommittedOrderStatus(s) {
			t.Fatalf("%q should be committed", s)
		}
	}
	notCommitted := []string{"draft", "cancelled", "", "  ", "unknown"}
	for _, s := range notCommitted {
		if IsCommittedOrderStatus(s) {
			t.Fatalf("%q should NOT be committed", s)
		}
	}
}

func TestMergeRequirements(t *testing.T) {
	lines := []OrderStockItem{
		{CatalogItemID: "a", WarehouseID: "w1", Qty: 2},
		{CatalogItemID: "a", WarehouseID: "w1", Qty: 3}, // same key merges -> 5
		{CatalogItemID: "a", WarehouseID: "w2", Qty: 1}, // different warehouse
		{CatalogItemID: "b", WarehouseID: "w1", Qty: 4},
		{CatalogItemID: "", WarehouseID: "w1", Qty: 9},  // empty item skipped
		{CatalogItemID: "c", WarehouseID: "w1", Qty: 0}, // zero qty skipped
	}
	got := mergeRequirements(lines)
	if len(got) != 3 {
		t.Fatalf("want 3 keys, got %d (%+v)", len(got), got)
	}
	if !approx(got[reqKey{"a", "w1"}], 5) {
		t.Fatalf("a/w1 = %v, want 5", got[reqKey{"a", "w1"}])
	}
	if !approx(got[reqKey{"a", "w2"}], 1) {
		t.Fatalf("a/w2 = %v, want 1", got[reqKey{"a", "w2"}])
	}
	if !approx(got[reqKey{"b", "w1"}], 4) {
		t.Fatalf("b/w1 = %v, want 4", got[reqKey{"b", "w1"}])
	}
}

func TestOrderCOGSDescription(t *testing.T) {
	orderID := "12345678-abcd-4ef0-8000-000000000001"
	got := orderCOGSDescription(orderID)
	want := "HPP pesanan #12345678 — harga pokok penjualan"
	if got != want {
		t.Fatalf("orderCOGSDescription() = %q, want %q", got, want)
	}
}

func TestOrderCOGSDescriptionShortID(t *testing.T) {
	got := orderCOGSDescription("abc")
	want := "HPP pesanan #abc — harga pokok penjualan"
	if got != want {
		t.Fatalf("orderCOGSDescription() = %q, want %q", got, want)
	}
}

func TestOrderCOGSReferencePrefix(t *testing.T) {
	orderID := "12345678-abcd-4ef0-8000-000000000001"
	ref := cogsRefPrefix + orderID
	if ref != "cogs:12345678-abcd-4ef0-8000-000000000001" {
		t.Fatalf("cogs reference = %q", ref)
	}
}
