package inventory

import "testing"

func TestCommittedStatusesForSQL(t *testing.T) {
	got := committedStatusesForSQL()
	if len(got) != len(committedOrderStatuses) {
		t.Fatalf("len = %d, want %d", len(got), len(committedOrderStatuses))
	}
	// must be sorted (stable SQL) and contain the committed set
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("not sorted: %v", got)
		}
	}
	for _, s := range got {
		if !IsCommittedOrderStatus(s) {
			t.Fatalf("%q not committed", s)
		}
	}
}

func TestFormatOrderRef(t *testing.T) {
	got := formatOrderRef("eb76635c-1234-5678-9abc-def012345678")
	if got != "WB-EB76635C" {
		t.Fatalf("formatOrderRef = %q, want WB-EB76635C", got)
	}
}

func TestAggregateSuggestedOpening(t *testing.T) {
	issues := []BackfillOrderIssue{
		{
			Shortages: []StockShortageLine{
				{CatalogItemID: "a", ItemName: "A", WarehouseID: "w1", WarehouseName: "Utama", QtyShort: 2},
				{CatalogItemID: "b", ItemName: "B", WarehouseID: "w1", WarehouseName: "Utama", QtyShort: 1},
			},
		},
		{
			Shortages: []StockShortageLine{
				{CatalogItemID: "a", ItemName: "A", WarehouseID: "w1", WarehouseName: "Utama", QtyShort: 5},
			},
		},
	}
	got := aggregateSuggestedOpening(issues)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	byItem := map[string]float64{}
	for _, s := range got {
		byItem[s.CatalogItemID] = s.MinQty
	}
	if byItem["a"] != 5 {
		t.Fatalf("min qty A = %g, want 5", byItem["a"])
	}
	if byItem["b"] != 1 {
		t.Fatalf("min qty B = %g, want 1", byItem["b"])
	}
}
