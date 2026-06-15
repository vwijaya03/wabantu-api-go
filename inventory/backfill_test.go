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

func TestCommittedStatusesIncludeOpsFlow(t *testing.T) {
	for _, s := range []string{"processing", "shipped", "completed"} {
		found := false
		for _, got := range committedStatusesForSQL() {
			if got == s {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("committedStatusesForSQL missing %q", s)
		}
	}
}

func TestOrderStockSyncDelta(t *testing.T) {
	item := reqKey{item: "a", warehouse: "w1"}
	cases := []struct {
		name     string
		required map[reqKey]float64
		net      map[reqKey]netEntry
		want     bool
	}{
		{
			name:     "belum ada issue",
			required: map[reqKey]float64{item: 3},
			net:      map[reqKey]netEntry{},
			want:     true,
		},
		{
			name:     "sudah sinkron",
			required: map[reqKey]float64{item: 3},
			net:      map[reqKey]netEntry{item: {qty: 3}},
			want:     false,
		},
		{
			name:     "ada sale_issue tapi belum cukup",
			required: map[reqKey]float64{item: 5},
			net:      map[reqKey]netEntry{item: {qty: 2}},
			want:     true,
		},
		{
			name:     "pernah restore stok net nol",
			required: map[reqKey]float64{item: 2},
			net:      map[reqKey]netEntry{item: {qty: 0}},
			want:     true,
		},
		{
			name:     "tanpa item terlacak",
			required: map[reqKey]float64{},
			net:      map[reqKey]netEntry{},
			want:     false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := orderStockSyncDelta(c.required, c.net); got != c.want {
				t.Fatalf("orderStockSyncDelta() = %v, want %v", got, c.want)
			}
		})
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
