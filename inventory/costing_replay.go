package inventory

// ReplayMovement is one historical movement fed to the recalculation engine.
// For "in" movements UnitCost is the receipt cost; for "out" it is recomputed.
type ReplayMovement struct {
	ID        string
	Direction string
	Qty       float64
	UnitCost  float64
}

// ReplaySnapshot is the recomputed result for one movement.
type ReplaySnapshot struct {
	MovementID string
	TotalCost  float64
	UnitCost   float64
	QtyAfter   float64
	AvgAfter   float64
}

// ReplayLayer is a remaining FIFO/LIFO cost layer after the full replay.
type ReplayLayer struct {
	SourceMovementID string
	QtyRemaining     float64
	UnitCost         float64
}

// ReplayResult is the full recomputed state for one (item, warehouse).
type ReplayResult struct {
	Snapshots  []ReplaySnapshot
	Layers     []ReplayLayer
	OnHand     float64
	AvgCost    float64
	TotalValue float64
}

// replayMovements deterministically recomputes balances, per-movement costs, and
// remaining cost layers from a chronological movement list under a costing method.
// Pure function — the heart of "Recalculate HPP", fully unit-testable.
//
// Zero-qty movements (e.g. revaluation) are skipped: recalculation rebuilds from
// receipt/issue history and intentionally does not preserve manual revaluations.
func replayMovements(movs []ReplayMovement, method string) ReplayResult {
	bal := BalanceState{}
	layers := make([]Layer, 0, len(movs))
	snaps := make([]ReplaySnapshot, 0, len(movs))

	for _, m := range movs {
		if m.Qty <= epsilon {
			continue
		}
		if m.Direction == dirIn {
			cost := round4(m.Qty * m.UnitCost)
			bal = applyIn(bal, m.Qty, m.UnitCost)
			if method != CostingAverage {
				layers = append(layers, Layer{ID: m.ID, QtyRemaining: m.Qty, UnitCost: m.UnitCost})
			}
			snaps = append(snaps, ReplaySnapshot{m.ID, cost, m.UnitCost, bal.OnHand, bal.AvgCost})
			continue
		}
		// out
		var cost float64
		if method == CostingAverage {
			cost = issueCostAverage(bal.AvgCost, m.Qty)
		} else {
			ordered := orderLayersForReplay(layers, method)
			draws, layerCost, shortfall := planConsumption(ordered, m.Qty)
			cost = layerCost
			if shortfall > epsilon {
				cost = round4(cost + shortfall*bal.AvgCost)
			}
			applyDrawsToLayers(layers, draws)
		}
		bal = applyOut(bal, m.Qty, cost)
		snaps = append(snaps, ReplaySnapshot{m.ID, cost, weightedUnitCost(cost, m.Qty), bal.OnHand, bal.AvgCost})
	}

	out := make([]ReplayLayer, 0)
	for _, l := range layers {
		if l.QtyRemaining > epsilon {
			out = append(out, ReplayLayer{SourceMovementID: l.ID, QtyRemaining: round4(l.QtyRemaining), UnitCost: l.UnitCost})
		}
	}
	return ReplayResult{Snapshots: snaps, Layers: out, OnHand: bal.OnHand, AvgCost: bal.AvgCost, TotalValue: bal.TotalValue}
}

// orderLayersForReplay returns layers with stock remaining, oldest-first for FIFO
// and newest-first for LIFO (layers slice is in chronological insertion order).
func orderLayersForReplay(layers []Layer, method string) []Layer {
	out := make([]Layer, 0, len(layers))
	if method == CostingLIFO {
		for i := len(layers) - 1; i >= 0; i-- {
			if layers[i].QtyRemaining > epsilon {
				out = append(out, layers[i])
			}
		}
		return out
	}
	for i := range layers {
		if layers[i].QtyRemaining > epsilon {
			out = append(out, layers[i])
		}
	}
	return out
}

// applyDrawsToLayers subtracts consumed quantities back onto the live layer slice.
func applyDrawsToLayers(layers []Layer, draws []LayerConsumption) {
	for _, d := range draws {
		for i := range layers {
			if layers[i].ID == d.LayerID {
				layers[i].QtyRemaining = round4(layers[i].QtyRemaining - d.Qty)
				break
			}
		}
	}
}
