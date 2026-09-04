package buyerflow

import "testing"

func TestIsOrderAmendMessage(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"jadikan 1 dengan pesanan sebelumnya", true},
		{"loh abon nutela ga masuk", true},
		{"berapa harga maggi?", false},
	}
	for _, tc := range cases {
		if got := IsOrderAmendMessage(tc.msg); got != tc.want {
			t.Fatalf("%q: want %v got %v", tc.msg, tc.want, got)
		}
	}
}

func TestExtractAmendLinesFromHistory(t *testing.T) {
	catalog := omahFoodCatalog()
	history := []Message{
		{Author: "contact", Body: "1 pcs, lalu abon sapi yang 250 gram 1pcs"},
		{Author: "contact", Body: "lalu nutela biskuit 1 piece"},
	}
	existing := map[string]bool{"maggi-percik": true}
	lines := ExtractAmendLinesFromHistory(history, catalog, existing, nil)
	if len(lines) != 2 {
		t.Fatalf("want 2 amend lines, got %d %+v", len(lines), lines)
	}
}

func TestShouldKeepCartOnExplicitNewOrder(t *testing.T) {
	unpinned := &OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "cadbury-mini",
		ProductName:   "Cadbury Mini",
		Qty:           1,
	}
	if !ShouldKeepCartOnExplicitNewOrder(unpinned, "pesanan baru") {
		t.Fatal("unpinned cart + pesanan baru must keep Redis cart")
	}
	pinned := *unpinned
	pinned.PersistedOrderID = "draft-maggi"
	if ShouldKeepCartOnExplicitNewOrder(&pinned, "pesanan baru") {
		t.Fatal("pinned leftover checkout must be cleared on pesanan baru")
	}
	if ShouldKeepCartOnExplicitNewOrder(unpinned, "mau tanya harga") {
		t.Fatal("non new-order text must not keep cart via this helper")
	}
}
