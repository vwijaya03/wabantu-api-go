package inventory

import "testing"

func TestAdjustmentPlan(t *testing.T) {
	mtype, dir, qty, ok := adjustmentPlan(5)
	if !ok || mtype != MovementAdjustPlus || dir != dirIn || !approx(qty, 5) {
		t.Fatalf("plus = (%q,%q,%v,%v)", mtype, dir, qty, ok)
	}
	mtype, dir, qty, ok = adjustmentPlan(-3.5)
	if !ok || mtype != MovementAdjustMinus || dir != dirOut || !approx(qty, 3.5) {
		t.Fatalf("minus = (%q,%q,%v,%v)", mtype, dir, qty, ok)
	}
	if _, _, _, ok := adjustmentPlan(0); ok {
		t.Fatal("zero qty should be rejected")
	}
	if _, _, _, ok := adjustmentPlan(1e-9); ok {
		t.Fatal("sub-epsilon qty should be rejected")
	}
}

func TestRevaluationDelta(t *testing.T) {
	// 100 units, old value 1.000.000 (avg 10.000). Reval to 9.000 -> new 900.000, delta -100.000.
	newTotal, delta := revaluationDelta(100, 1000000, 9000)
	if !approx(newTotal, 900000) || !approx(delta, -100000) {
		t.Fatalf("down: newTotal=%v delta=%v", newTotal, delta)
	}
	// Reval up to 11.000 -> new 1.100.000, delta +100.000.
	newTotal, delta = revaluationDelta(100, 1000000, 11000)
	if !approx(newTotal, 1100000) || !approx(delta, 100000) {
		t.Fatalf("up: newTotal=%v delta=%v", newTotal, delta)
	}
	// No change.
	_, delta = revaluationDelta(100, 1000000, 10000)
	if !approx(delta, 0) {
		t.Fatalf("flat delta = %v, want 0", delta)
	}
	// From zero value (no prior cost).
	newTotal, delta = revaluationDelta(50, 0, 8000)
	if !approx(newTotal, 400000) || !approx(delta, 400000) {
		t.Fatalf("from-zero: newTotal=%v delta=%v", newTotal, delta)
	}
}

func TestAbsHelper(t *testing.T) {
	if abs(-5) != 5 || abs(5) != 5 || abs(0) != 0 {
		t.Fatal("abs broken")
	}
}

func TestValidateOpeningBalanceEntryPairs(t *testing.T) {
	err := validateOpeningBalanceEntryPairs([]OpeningEntry{
		{CatalogItemID: "a", WarehouseID: "w1", Qty: 1, UnitCost: 1},
		{CatalogItemID: "a", WarehouseID: "w1", Qty: 2, UnitCost: 1},
	})
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	if err := validateOpeningBalanceEntryPairs([]OpeningEntry{
		{CatalogItemID: "a", WarehouseID: "w1", Qty: 1, UnitCost: 1},
		{CatalogItemID: "a", WarehouseID: "w2", Qty: 1, UnitCost: 1},
	}); err != nil {
		t.Fatalf("different warehouse should pass: %v", err)
	}
}
