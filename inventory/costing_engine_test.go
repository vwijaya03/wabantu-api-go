package inventory

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-4 }

func TestPlanConsumptionSingleLayerExact(t *testing.T) {
	layers := []Layer{{ID: "L1", QtyRemaining: 10, UnitCost: 5000}}
	draws, total, short := planConsumption(layers, 10)
	if short != 0 {
		t.Fatalf("shortfall = %v, want 0", short)
	}
	if !approx(total, 50000) {
		t.Fatalf("total = %v, want 50000", total)
	}
	if len(draws) != 1 || draws[0].LayerID != "L1" || !approx(draws[0].Qty, 10) {
		t.Fatalf("draws = %+v", draws)
	}
}

func TestPlanConsumptionFIFOAcrossLayers(t *testing.T) {
	// Salmon FIFO: older Rp10.000 first, then Rp12.000.
	layers := []Layer{
		{ID: "old", QtyRemaining: 6, UnitCost: 10000},
		{ID: "new", QtyRemaining: 10, UnitCost: 12000},
	}
	draws, total, short := planConsumption(layers, 9)
	if short != 0 {
		t.Fatalf("shortfall = %v, want 0", short)
	}
	// 6 @10000 + 3 @12000 = 60000 + 36000 = 96000
	if !approx(total, 96000) {
		t.Fatalf("total = %v, want 96000", total)
	}
	if len(draws) != 2 {
		t.Fatalf("want 2 draws, got %d (%+v)", len(draws), draws)
	}
	if draws[0].LayerID != "old" || !approx(draws[0].Qty, 6) {
		t.Fatalf("first draw = %+v, want 6 from old", draws[0])
	}
	if draws[1].LayerID != "new" || !approx(draws[1].Qty, 3) {
		t.Fatalf("second draw = %+v, want 3 from new", draws[1])
	}
}

func TestPlanConsumptionLIFOOrdering(t *testing.T) {
	// LIFO: caller passes newest-first. Same layers, opposite order.
	layers := []Layer{
		{ID: "new", QtyRemaining: 10, UnitCost: 12000},
		{ID: "old", QtyRemaining: 6, UnitCost: 10000},
	}
	draws, total, short := planConsumption(layers, 9)
	if short != 0 {
		t.Fatalf("shortfall = %v", short)
	}
	// 9 @12000 = 108000 (all from newest)
	if !approx(total, 108000) {
		t.Fatalf("total = %v, want 108000", total)
	}
	if len(draws) != 1 || draws[0].LayerID != "new" {
		t.Fatalf("draws = %+v, want single draw from new", draws)
	}
}

func TestPlanConsumptionShortfall(t *testing.T) {
	layers := []Layer{{ID: "L1", QtyRemaining: 4, UnitCost: 5000}}
	draws, total, short := planConsumption(layers, 10)
	if !approx(short, 6) {
		t.Fatalf("shortfall = %v, want 6", short)
	}
	if !approx(total, 20000) {
		t.Fatalf("total = %v, want 20000 (only what exists)", total)
	}
	if len(draws) != 1 {
		t.Fatalf("draws = %+v", draws)
	}
}

func TestPlanConsumptionSkipsDepletedLayers(t *testing.T) {
	layers := []Layer{
		{ID: "empty", QtyRemaining: 0, UnitCost: 9999},
		{ID: "good", QtyRemaining: 5, UnitCost: 3000},
	}
	draws, total, short := planConsumption(layers, 5)
	if short != 0 || !approx(total, 15000) {
		t.Fatalf("total=%v short=%v", total, short)
	}
	if len(draws) != 1 || draws[0].LayerID != "good" {
		t.Fatalf("draws = %+v, want only 'good'", draws)
	}
}

func TestPlanConsumptionEmptyLayers(t *testing.T) {
	draws, total, short := planConsumption(nil, 5)
	if !approx(short, 5) || total != 0 || len(draws) != 0 {
		t.Fatalf("empty layers: draws=%+v total=%v short=%v", draws, total, short)
	}
}

func TestPlanConsumptionFractionalQty(t *testing.T) {
	// Salmon by kg: 1.5 kg from a 2 kg layer at Rp80.000/kg.
	layers := []Layer{{ID: "kg", QtyRemaining: 2, UnitCost: 80000}}
	draws, total, short := planConsumption(layers, 1.5)
	if short != 0 || !approx(total, 120000) {
		t.Fatalf("total=%v short=%v", total, short)
	}
	if len(draws) != 1 || !approx(draws[0].Qty, 1.5) {
		t.Fatalf("draws=%+v", draws)
	}
}

func TestApplyReceiptAverageFirstReceipt(t *testing.T) {
	onHand, avg := applyReceiptAverage(0, 0, 10, 5000)
	if !approx(onHand, 10) || !approx(avg, 5000) {
		t.Fatalf("onHand=%v avg=%v, want 10/5000", onHand, avg)
	}
}

func TestApplyReceiptAverageBlended(t *testing.T) {
	// 10 @5000 then 10 @7000 -> 20 @6000
	onHand, avg := applyReceiptAverage(10, 5000, 10, 7000)
	if !approx(onHand, 20) || !approx(avg, 6000) {
		t.Fatalf("onHand=%v avg=%v, want 20/6000", onHand, avg)
	}
}

func TestApplyReceiptAverageBlendedUneven(t *testing.T) {
	// 3 @10000 + 1 @14000 -> 4 units, avg = (30000+14000)/4 = 11000
	onHand, avg := applyReceiptAverage(3, 10000, 1, 14000)
	if !approx(onHand, 4) || !approx(avg, 11000) {
		t.Fatalf("onHand=%v avg=%v, want 4/11000", onHand, avg)
	}
}

func TestIssueCostAverage(t *testing.T) {
	if got := issueCostAverage(6000, 3); !approx(got, 18000) {
		t.Fatalf("issueCostAverage = %v, want 18000", got)
	}
}

func TestWeightedUnitCost(t *testing.T) {
	if got := weightedUnitCost(96000, 9); !approx(got, 10666.6667) {
		t.Fatalf("weightedUnitCost = %v, want ~10666.67", got)
	}
	if got := weightedUnitCost(100, 0); got != 0 {
		t.Fatalf("weightedUnitCost div-zero = %v, want 0", got)
	}
}

func TestRoundHelpers(t *testing.T) {
	if round4(1.234567) != 1.2346 {
		t.Fatalf("round4 = %v", round4(1.234567))
	}
	if round2(1.235) != 1.24 {
		t.Fatalf("round2 = %v", round2(1.235))
	}
}

func TestApplyInBalance(t *testing.T) {
	b := applyIn(BalanceState{}, 10, 5000)
	if !approx(b.OnHand, 10) || !approx(b.AvgCost, 5000) || !approx(b.TotalValue, 50000) {
		t.Fatalf("applyIn first = %+v", b)
	}
	b = applyIn(b, 10, 7000)
	if !approx(b.OnHand, 20) || !approx(b.AvgCost, 6000) || !approx(b.TotalValue, 120000) {
		t.Fatalf("applyIn blended = %+v", b)
	}
}

func TestApplyOutBalanceAveragePreserved(t *testing.T) {
	// AVERAGE: out cost = avg*qty keeps avg constant.
	b := BalanceState{OnHand: 20, AvgCost: 6000, TotalValue: 120000}
	out := applyOut(b, 5, issueCostAverage(6000, 5))
	if !approx(out.OnHand, 15) || !approx(out.AvgCost, 6000) || !approx(out.TotalValue, 90000) {
		t.Fatalf("applyOut average = %+v, want 15/6000/90000", out)
	}
}

func TestApplyOutBalanceDepleted(t *testing.T) {
	b := BalanceState{OnHand: 5, AvgCost: 6000, TotalValue: 30000}
	out := applyOut(b, 5, 30000)
	if !approx(out.OnHand, 0) || !approx(out.TotalValue, 0) {
		t.Fatalf("applyOut depleted = %+v, want 0 onHand & value", out)
	}
}

func TestApplyOutBalanceFIFORecomputesAvg(t *testing.T) {
	// FIFO: 20 units value 120000 (avg 6000). Issue 6 oldest @ cost 60000.
	// Remaining 14 units value 60000 -> avg ~4285.71.
	b := BalanceState{OnHand: 20, AvgCost: 6000, TotalValue: 120000}
	out := applyOut(b, 6, 60000)
	if !approx(out.OnHand, 14) || !approx(out.TotalValue, 60000) {
		t.Fatalf("applyOut fifo = %+v", out)
	}
	if !approx(out.AvgCost, 4285.7143) {
		t.Fatalf("avg = %v, want ~4285.71", out.AvgCost)
	}
}

// End-to-end FIFO lifecycle on the pure layer model (no DB):
// receive two batches, issue across both, verify remaining + COGS.
func TestFIFOLifecyclePure(t *testing.T) {
	layers := []Layer{
		{ID: "b1", QtyRemaining: 5, UnitCost: 10000},
		{ID: "b2", QtyRemaining: 5, UnitCost: 12000},
	}
	draws, cogs, short := planConsumption(layers, 7)
	if short != 0 {
		t.Fatalf("short=%v", short)
	}
	// 5@10000 + 2@12000 = 74000
	if !approx(cogs, 74000) {
		t.Fatalf("cogs=%v want 74000", cogs)
	}
	// Apply draws back to layers (what the DB engine will persist).
	rem := map[string]float64{"b1": 5, "b2": 5}
	for _, d := range draws {
		rem[d.LayerID] -= d.Qty
	}
	if !approx(rem["b1"], 0) || !approx(rem["b2"], 3) {
		t.Fatalf("remaining b1=%v b2=%v want 0/3", rem["b1"], rem["b2"])
	}
}
