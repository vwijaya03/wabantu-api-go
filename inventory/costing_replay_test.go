package inventory

import "testing"

func TestReplayFIFO(t *testing.T) {
	movs := []ReplayMovement{
		{ID: "r1", Direction: dirIn, Qty: 5, UnitCost: 10000},
		{ID: "r2", Direction: dirIn, Qty: 5, UnitCost: 12000},
		{ID: "s1", Direction: dirOut, Qty: 7}, // 5@10000 + 2@12000 = 74000
	}
	res := replayMovements(movs, CostingFIFO)
	if !approx(res.OnHand, 3) {
		t.Fatalf("onHand = %v, want 3", res.OnHand)
	}
	// remaining layer: 3 @ 12000 (value 36000), avg 12000
	if !approx(res.TotalValue, 36000) || !approx(res.AvgCost, 12000) {
		t.Fatalf("value=%v avg=%v want 36000/12000", res.TotalValue, res.AvgCost)
	}
	if len(res.Layers) != 1 || !approx(res.Layers[0].QtyRemaining, 3) || !approx(res.Layers[0].UnitCost, 12000) {
		t.Fatalf("layers = %+v", res.Layers)
	}
	// per-movement COGS for s1
	var s1 ReplaySnapshot
	for _, s := range res.Snapshots {
		if s.MovementID == "s1" {
			s1 = s
		}
	}
	if !approx(s1.TotalCost, 74000) {
		t.Fatalf("s1 cost = %v, want 74000", s1.TotalCost)
	}
}

func TestReplayLIFO(t *testing.T) {
	movs := []ReplayMovement{
		{ID: "r1", Direction: dirIn, Qty: 5, UnitCost: 10000},
		{ID: "r2", Direction: dirIn, Qty: 5, UnitCost: 12000},
		{ID: "s1", Direction: dirOut, Qty: 7}, // 5@12000 + 2@10000 = 80000
	}
	res := replayMovements(movs, CostingLIFO)
	if !approx(res.OnHand, 3) {
		t.Fatalf("onHand = %v", res.OnHand)
	}
	// remaining 3 @ 10000 = 30000
	if !approx(res.TotalValue, 30000) {
		t.Fatalf("value = %v, want 30000", res.TotalValue)
	}
	var s1 ReplaySnapshot
	for _, s := range res.Snapshots {
		if s.MovementID == "s1" {
			s1 = s
		}
	}
	if !approx(s1.TotalCost, 80000) {
		t.Fatalf("s1 cost = %v, want 80000", s1.TotalCost)
	}
}

func TestReplayAverage(t *testing.T) {
	movs := []ReplayMovement{
		{ID: "r1", Direction: dirIn, Qty: 10, UnitCost: 5000},
		{ID: "r2", Direction: dirIn, Qty: 10, UnitCost: 7000}, // avg 6000
		{ID: "s1", Direction: dirOut, Qty: 5},                 // 5*6000 = 30000
	}
	res := replayMovements(movs, CostingAverage)
	if !approx(res.OnHand, 15) || !approx(res.AvgCost, 6000) {
		t.Fatalf("onHand=%v avg=%v want 15/6000", res.OnHand, res.AvgCost)
	}
	if len(res.Layers) != 0 {
		t.Fatalf("average keeps no layers, got %d", len(res.Layers))
	}
	var s1 ReplaySnapshot
	for _, s := range res.Snapshots {
		if s.MovementID == "s1" {
			s1 = s
		}
	}
	if !approx(s1.TotalCost, 30000) {
		t.Fatalf("s1 cost = %v, want 30000", s1.TotalCost)
	}
}

func TestReplaySkipsZeroQty(t *testing.T) {
	movs := []ReplayMovement{
		{ID: "r1", Direction: dirIn, Qty: 5, UnitCost: 10000},
		{ID: "reval", Direction: dirIn, Qty: 0, UnitCost: 99999}, // skipped
	}
	res := replayMovements(movs, CostingFIFO)
	if !approx(res.OnHand, 5) || !approx(res.AvgCost, 10000) {
		t.Fatalf("onHand=%v avg=%v", res.OnHand, res.AvgCost)
	}
	if len(res.Snapshots) != 1 {
		t.Fatalf("zero-qty movement should be skipped, snaps=%d", len(res.Snapshots))
	}
}

func TestReplayConsistencyWithIncrementalFIFO(t *testing.T) {
	// Recalc must match incremental engine: receive then partial issues.
	movs := []ReplayMovement{
		{ID: "r1", Direction: dirIn, Qty: 8, UnitCost: 10000},
		{ID: "s1", Direction: dirOut, Qty: 3},
		{ID: "r2", Direction: dirIn, Qty: 4, UnitCost: 15000},
		{ID: "s2", Direction: dirOut, Qty: 6}, // 5@10000 + 1@15000 = 65000
	}
	res := replayMovements(movs, CostingFIFO)
	// after: in 8+4=12, out 3+6=9 -> on hand 3 @ 15000 = 45000
	if !approx(res.OnHand, 3) || !approx(res.TotalValue, 45000) {
		t.Fatalf("onHand=%v value=%v want 3/45000", res.OnHand, res.TotalValue)
	}
}
