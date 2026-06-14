package inventory

import "math"

// epsilon guards float comparisons; quantities/costs are stored as NUMERIC(18,4).
const epsilon = 1e-6

// round4 rounds to 4 decimal places (matches NUMERIC(18,4) storage).
func round4(x float64) float64 {
	return math.Round(x*1e4) / 1e4
}

// round2 rounds to 2 decimals (finance amounts post as NUMERIC(18,2)).
func round2(x float64) float64 {
	return math.Round(x*1e2) / 1e2
}

// Layer is an in-memory cost layer used to plan FIFO/LIFO consumption.
// The caller orders the slice (oldest-first for FIFO, newest-first for LIFO).
type Layer struct {
	ID           string
	QtyRemaining float64
	UnitCost     float64
	BatchNo      string
}

// LayerConsumption is the amount drawn from a single cost layer during an issue.
type LayerConsumption struct {
	LayerID  string
	Qty      float64
	UnitCost float64
}

// planConsumption draws `qty` from pre-ordered layers and returns the per-layer
// draw, the exact total cost (COGS), and any shortfall that could not be filled.
//
// shortfall == 0 means the issue is fully covered. A positive shortfall means the
// layers ran out (caller decides whether to block based on block_negative_stock).
//
// This is a pure function: the core of FIFO/LIFO costing, fully unit-testable.
func planConsumption(ordered []Layer, qty float64) (draws []LayerConsumption, totalCost float64, shortfall float64) {
	remaining := qty
	for _, l := range ordered {
		if remaining <= epsilon {
			break
		}
		if l.QtyRemaining <= epsilon {
			continue
		}
		take := l.QtyRemaining
		if take > remaining {
			take = remaining
		}
		draws = append(draws, LayerConsumption{LayerID: l.ID, Qty: round4(take), UnitCost: l.UnitCost})
		totalCost += take * l.UnitCost
		remaining -= take
	}
	if remaining > epsilon {
		shortfall = round4(remaining)
	}
	return draws, round4(totalCost), shortfall
}

// applyReceiptAverage returns the new on-hand quantity and weighted-average unit
// cost after receiving `recvQty` at `recvUnitCost`. Used by the AVERAGE method.
func applyReceiptAverage(onHand, avgCost, recvQty, recvUnitCost float64) (newOnHand, newAvg float64) {
	newOnHand = onHand + recvQty
	if newOnHand <= epsilon {
		return 0, 0
	}
	newAvg = (onHand*avgCost + recvQty*recvUnitCost) / newOnHand
	return round4(newOnHand), round4(newAvg)
}

// issueCostAverage returns the COGS of issuing `qty` at the current average cost.
// The average is unchanged by an issue; on-hand decreases (handled by caller).
func issueCostAverage(avgCost, qty float64) float64 {
	return round4(avgCost * qty)
}

// weightedUnitCost computes total/qty guarding division by zero (for movement rows).
func weightedUnitCost(totalCost, qty float64) float64 {
	if qty <= epsilon {
		return 0
	}
	return round4(totalCost / qty)
}

// BalanceState is the fast-read snapshot of stock per (item, warehouse).
// FIFO/LIFO costing precision lives in cost layers; this snapshot tracks
// on-hand + total value and derives an average for display/AVERAGE method.
type BalanceState struct {
	OnHand     float64
	AvgCost    float64
	TotalValue float64
}

// applyIn returns the new snapshot after an incoming qty at unitCost.
func applyIn(b BalanceState, qty, unitCost float64) BalanceState {
	onHand := b.OnHand + qty
	total := b.TotalValue + qty*unitCost
	avg := 0.0
	if onHand > epsilon {
		avg = total / onHand
	}
	return BalanceState{OnHand: round4(onHand), AvgCost: round4(avg), TotalValue: round4(total)}
}

// applyOut returns the new snapshot after an outgoing qty costing `cost` total.
// For the AVERAGE method (cost == avg*qty) this keeps the average unchanged.
func applyOut(b BalanceState, qty, cost float64) BalanceState {
	onHand := b.OnHand - qty
	total := b.TotalValue - cost
	if onHand <= epsilon {
		return BalanceState{OnHand: round4(onHand), AvgCost: b.AvgCost, TotalValue: 0}
	}
	return BalanceState{OnHand: round4(onHand), AvgCost: round4(total / onHand), TotalValue: round4(total)}
}
