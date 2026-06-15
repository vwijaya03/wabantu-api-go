package inventory

import (
	"context"
	"database/sql"

	appErrs "encore.app/wabantu/shared/errs"
)

// replayPairInTx rebuilds cost layers, movement costs, and balance for one
// (item, warehouse) from remaining ledger rows. Used after hard-deleting movements.
func replayPairInTx(ctx context.Context, tx *sql.Tx, item, warehouse string) error {
	cc, err := loadCostingContext(ctx, tx, item)
	if err != nil {
		return err
	}
	movs, err := loadReplayMovements(ctx, tx, item, warehouse)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	res := replayMovements(movs, cc.method)

	for _, s := range res.Snapshots {
		if _, err := tx.ExecContext(ctx, `
			UPDATE inv_stock_movement
			SET total_cost = $2, unit_cost = $3, qty_after = $4, avg_cost_after = $5
			WHERE id = $1`, s.MovementID, s.TotalCost, s.UnitCost, s.QtyAfter, s.AvgAfter); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM inv_cost_layer WHERE catalog_item_id = $1 AND warehouse_id = $2`,
		item, warehouse); err != nil {
		return appErrs.Internal(err.Error())
	}
	for _, l := range res.Layers {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inv_cost_layer
			  (catalog_item_id, warehouse_id, qty_remaining, unit_cost, source_movement_id, received_at)
			VALUES ($1,$2,$3,$4,$5, now())`,
			item, warehouse, l.QtyRemaining, l.UnitCost, nullUUID(l.SourceMovementID)); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO inv_stock_balance (catalog_item_id, warehouse_id, on_hand, avg_unit_cost, total_value, updated_at)
		VALUES ($1,$2,$3,$4,$5, now())
		ON CONFLICT (catalog_item_id, warehouse_id)
		DO UPDATE SET on_hand = $3, avg_unit_cost = $4, total_value = $5, updated_at = now()`,
		item, warehouse, res.OnHand, res.AvgCost, res.TotalValue); err != nil {
		return appErrs.Internal(err.Error())
	}
	return nil
}

func replayPairsInTx(ctx context.Context, tx *sql.Tx, pairs []itemWarehouse) error {
	seen := map[itemWarehouse]struct{}{}
	for _, p := range pairs {
		if p.item == "" || p.warehouse == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		if err := replayPairInTx(ctx, tx, p.item, p.warehouse); err != nil {
			return err
		}
	}
	return nil
}

type movementRef struct {
	id, item, warehouse string
}

func collectMovementsByRef(ctx context.Context, q querier, refType, refID string) ([]movementRef, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id::text, catalog_item_id::text, warehouse_id::text
		FROM inv_stock_movement
		WHERE ref_type = $1 AND ref_id = $2::uuid`, refType, refID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	var out []movementRef
	for rows.Next() {
		var m movementRef
		if err := rows.Scan(&m.id, &m.item, &m.warehouse); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func pairsFromMovements(movs []movementRef) []itemWarehouse {
	out := make([]itemWarehouse, 0, len(movs))
	seen := map[itemWarehouse]struct{}{}
	for _, m := range movs {
		p := itemWarehouse{m.item, m.warehouse}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// purgeMovementsByRef hard-deletes movements for a document reference and replays
// affected balances.
func purgeMovementsByRef(ctx context.Context, tx *sql.Tx, refType, refID string) ([]itemWarehouse, error) {
	movs, err := collectMovementsByRef(ctx, tx, refType, refID)
	if err != nil {
		return nil, err
	}
	pairs := pairsFromMovements(movs)
	if len(movs) > 0 {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM inv_stock_movement WHERE ref_type = $1 AND ref_id = $2::uuid`,
			refType, refID); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	if err := replayPairsInTx(ctx, tx, pairs); err != nil {
		return nil, err
	}
	return pairs, nil
}
