package inventory

import "testing"

func TestSumBillLines(t *testing.T) {
	lines := []BillLineInput{
		{Qty: 10, UnitCost: 5000},
		{Qty: 3, UnitCost: 12000},
		{Qty: 1.5, UnitCost: 80000}, // fractional (kg)
	}
	// 50000 + 36000 + 120000 = 206000
	if got := sumBillLines(lines); !approx(got, 206000) {
		t.Fatalf("sumBillLines = %v, want 206000", got)
	}
	if got := sumBillLines(nil); got != 0 {
		t.Fatalf("empty = %v, want 0", got)
	}
}
