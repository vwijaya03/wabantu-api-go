package inventory

import (
	"context"
	"database/sql"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

type RecalcParams struct {
	CatalogItemID string `json:"catalogItemId"` // optional; empty = all items
}

type RecalcResponse struct {
	Recomputed int `json:"recomputed"` // number of (item, warehouse) pairs rebuilt
}

// RecalculateHPP replays the movement ledger to rebuild cost layers, balances, and
// per-movement costs under each item's current costing method. The maintenance tool
// for "HPP kacau". Owner-only. Does not preserve manual revaluations (rebuilds from
// receipt/issue history).
//
//encore:api auth method=POST path=/api/v1/inventory/recalculate
func RecalculateHPP(ctx context.Context, p *RecalcParams) (*RecalcResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	pairs, err := movementPairs(ctx, conn, strings.TrimSpace(p.CatalogItemID))
	if err != nil {
		return nil, err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	recomputed := 0
	ccl := newCostingContextLoader()
	for _, pr := range pairs {
		cc, cerr := ccl.load(ctx, tx, pr.item)
		if cerr != nil {
			return nil, cerr
		}
		movs, lerr := loadReplayMovements(ctx, tx, pr.item, pr.warehouse)
		if lerr != nil {
			return nil, appErrs.Internal(lerr.Error())
		}
		res := replayMovements(movs, cc.method)

		if err := applyMovementSnapshots(ctx, tx, res.Snapshots); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM inv_cost_layer WHERE catalog_item_id = $1 AND warehouse_id = $2`,
			pr.item, pr.warehouse); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		for _, l := range res.Layers {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO inv_cost_layer
				  (catalog_item_id, warehouse_id, qty_remaining, unit_cost, source_movement_id, received_at)
				VALUES ($1,$2,$3,$4,$5, now())`,
				pr.item, pr.warehouse, l.QtyRemaining, l.UnitCost, nullUUID(l.SourceMovementID)); err != nil {
				return nil, appErrs.Internal(err.Error())
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inv_stock_balance (catalog_item_id, warehouse_id, on_hand, avg_unit_cost, total_value, updated_at)
			VALUES ($1,$2,$3,$4,$5, now())
			ON CONFLICT (catalog_item_id, warehouse_id)
			DO UPDATE SET on_hand = $3, avg_unit_cost = $4, total_value = $5, updated_at = now()`,
			pr.item, pr.warehouse, res.OnHand, res.AvgCost, res.TotalValue); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		recomputed++
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &RecalcResponse{Recomputed: recomputed}, nil
}

type itemWarehouse struct{ item, warehouse string }

func movementPairs(ctx context.Context, conn *sql.Conn, catalogItemID string) ([]itemWarehouse, error) {
	q := `SELECT DISTINCT catalog_item_id::text, warehouse_id::text FROM inv_stock_movement`
	args := []any{}
	if catalogItemID != "" {
		q += ` WHERE catalog_item_id = $1`
		args = append(args, catalogItemID)
	}
	rows, err := conn.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	var out []itemWarehouse
	for rows.Next() {
		var p itemWarehouse
		if err := rows.Scan(&p.item, &p.warehouse); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func loadReplayMovements(ctx context.Context, tx *sql.Tx, item, warehouse string) ([]ReplayMovement, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id::text, direction, qty, unit_cost
		FROM inv_stock_movement
		WHERE catalog_item_id = $1 AND warehouse_id = $2
		ORDER BY created_at, id`, item, warehouse)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReplayMovement
	for rows.Next() {
		var m ReplayMovement
		if err := rows.Scan(&m.ID, &m.Direction, &m.Qty, &m.UnitCost); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
