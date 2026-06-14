package inventory

import "testing"

func TestReturnableQty(t *testing.T) {
	cases := []struct {
		sold, returned, want float64
	}{
		{10, 0, 10},
		{10, 4, 6},
		{10, 10, 0},
		{10, 12, 0}, // over-returned clamps at 0
		{1.5, 0.5, 1},
		{0, 0, 0},
	}
	for _, c := range cases {
		if got := returnableQty(c.sold, c.returned); !approx(got, c.want) {
			t.Fatalf("returnableQty(%v,%v) = %v, want %v", c.sold, c.returned, got, c.want)
		}
	}
}

func TestWeightedItemCost(t *testing.T) {
	m := map[string]netEntry{
		"a": {qty: 10, cost: 50000}, // avg 5000
		"b": {qty: 0, cost: 0},      // guard div-by-zero
	}
	if got := weightedItemCost(m, "a"); !approx(got, 5000) {
		t.Fatalf("weightedItemCost(a) = %v, want 5000", got)
	}
	if got := weightedItemCost(m, "b"); got != 0 {
		t.Fatalf("weightedItemCost(b) = %v, want 0", got)
	}
	if got := weightedItemCost(m, "missing"); got != 0 {
		t.Fatalf("weightedItemCost(missing) = %v, want 0", got)
	}
}
