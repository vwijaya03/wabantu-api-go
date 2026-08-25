package inventory

import (
	"context"
	"database/sql"
	"time"

	appdb "encore.app/wabantu/shared/db"
	appErrs "encore.app/wabantu/shared/errs"
)

// ensureStockTxnBackfill creates inv_stock_transaction headers for legacy movements
// that predate the transaction-header table (idempotent).
func ensureStockTxnBackfill(ctx context.Context, sch appdb.SchemaSQL, q querier, tenantSchema string) error {
	if isStockTxnBackfillDoneCached(tenantSchema) {
		return nil
	}
	done, err := isStockTxnBackfillDone(ctx, sch, q)
	if err != nil {
		return err
	}
	if done {
		markStockTxnBackfillDoneCached(tenantSchema)
		return nil
	}
	var ready bool
	if err := qrow(ctx, sch, q, `
		SELECT EXISTS (
		  SELECT 1 FROM information_schema.tables
		  WHERE table_schema = current_schema() AND table_name = 'inv_stock_transaction'
		)`).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		if err := markStockTxnBackfillDone(ctx, sch, q); err != nil {
			return err
		}
		markStockTxnBackfillDoneCached(tenantSchema)
		return nil
	}
	var hasOrphans bool
	if err := qrow(ctx, sch, q, `
		SELECT EXISTS (
		  SELECT 1 FROM inv_stock_movement
		  WHERE ref_id IS NULL
		    AND movement_type IN ('adjustment_plus','adjustment_minus','opening_balance','transfer_out','revaluation_cost')
		  LIMIT 1
		)`).Scan(&hasOrphans); err != nil {
		return err
	}
	if !hasOrphans {
		if err := markStockTxnBackfillDone(ctx, sch, q); err != nil {
			return err
		}
		markStockTxnBackfillDoneCached(tenantSchema)
		return nil
	}
	if err := backfillStockTransactionHeaders(ctx, sch); err != nil {
		return err
	}
	if err := markStockTxnBackfillDone(ctx, sch, q); err != nil {
		return err
	}
	markStockTxnBackfillDoneCached(tenantSchema)
	return nil
}

func isStockTxnBackfillDone(ctx context.Context, sch appdb.SchemaSQL, q querier) (bool, error) {
	var done bool
	err := qrow(ctx, sch, q, `
		SELECT COALESCE(stock_txn_backfill_done, false)
		FROM inv_setting ORDER BY created_at LIMIT 1`).Scan(&done)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		// column may not exist on tenants mid-migration
		return false, nil
	}
	return done, nil
}

func markStockTxnBackfillDone(ctx context.Context, sch appdb.SchemaSQL, q querier) error {
	_, err := qexec(ctx, sch, q, `
		UPDATE inv_setting SET stock_txn_backfill_done = true, updated_at = now()
		WHERE id = (SELECT id FROM inv_setting ORDER BY created_at LIMIT 1)`)
	return err
}

func backfillStockTransactionHeaders(ctx context.Context, sch appdb.SchemaSQL) error {
	pool := tenantDB()
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	if err := backfillAdjustmentHeaders(ctx, sch, tx); err != nil {
		return err
	}
	if err := backfillRevaluationHeaders(ctx, sch, tx); err != nil {
		return err
	}
	if err := backfillOpeningHeaders(ctx, sch, tx); err != nil {
		return err
	}
	if err := backfillTransferHeaders(ctx, sch, tx); err != nil {
		return err
	}
	return tx.Commit()
}

type backfillAdjRow struct {
	movID, item, wh, mtype, note, createdBy, txnDate string
	qty, unitCost                                   float64
}

func backfillAdjustmentHeaders(ctx context.Context, sch appdb.SchemaSQL, tx *sql.Tx) error {
	rows, err := qquery(ctx, sch, tx, `
		SELECT id::text, catalog_item_id::text, warehouse_id::text, movement_type, qty, unit_cost,
		       COALESCE(note,''), COALESCE(created_by::text,''), created_at::date
		FROM inv_stock_movement m
		WHERE m.ref_id IS NULL AND m.movement_type IN ('adjustment_plus','adjustment_minus')
		ORDER BY m.created_at`)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	items, err := scanBackfillAdjRows(rows)
	if err != nil {
		return err
	}
	for _, r := range items {
		signed := r.qty
		if r.mtype == MovementAdjustMinus {
			signed = -r.qty
		}
		txnID, _, err := insertStockTransactionHeader(ctx, sch, tx, TxnKindAdjustment, r.txnDate, r.note, r.createdBy)
		if err != nil {
			return err
		}
		if _, err := qexec(ctx, sch, tx, `
			UPDATE inv_stock_transaction
			SET catalog_item_id = $2, warehouse_id = $3, signed_qty = $4, unit_cost = $5, note = $6
			WHERE id = $1`,
			txnID, r.item, r.wh, signed, r.unitCost, nullStr(r.note)); err != nil {
			return appErrs.Internal(err.Error())
		}
		if _, err := qexec(ctx, sch, tx,
			`UPDATE inv_stock_movement SET ref_type = $2, ref_id = $3::uuid WHERE id = $1`,
			r.movID, TxnKindAdjustment, txnID); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	return nil
}

func scanBackfillAdjRows(rows *sql.Rows) ([]backfillAdjRow, error) {
	defer rows.Close()
	out := make([]backfillAdjRow, 0)
	for rows.Next() {
		var r backfillAdjRow
		if err := rows.Scan(&r.movID, &r.item, &r.wh, &r.mtype, &r.qty, &r.unitCost, &r.note, &r.createdBy, &r.txnDate); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return out, nil
}

type backfillRevalRow struct {
	movID, item, wh, note, createdBy, txnDate string
	newCost                                     float64
}

func backfillRevaluationHeaders(ctx context.Context, sch appdb.SchemaSQL, tx *sql.Tx) error {
	rows, err := qquery(ctx, sch, tx, `
		SELECT id::text, catalog_item_id::text, warehouse_id::text, unit_cost,
		       COALESCE(note,''), COALESCE(created_by::text,''), created_at::date
		FROM inv_stock_movement m
		WHERE m.ref_id IS NULL AND m.movement_type = 'revaluation_cost'
		ORDER BY m.created_at`)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	items, err := scanBackfillRevalRows(rows)
	if err != nil {
		return err
	}
	for _, r := range items {
		txnID, _, err := insertStockTransactionHeader(ctx, sch, tx, TxnKindRevaluation, r.txnDate, r.note, r.createdBy)
		if err != nil {
			return err
		}
		if _, err := qexec(ctx, sch, tx, `
			UPDATE inv_stock_transaction
			SET catalog_item_id = $2, warehouse_id = $3, new_unit_cost = $4, note = $5
			WHERE id = $1`,
			txnID, r.item, r.wh, r.newCost, nullStr(r.note)); err != nil {
			return appErrs.Internal(err.Error())
		}
		if _, err := qexec(ctx, sch, tx,
			`UPDATE inv_stock_movement SET ref_type = $2, ref_id = $3::uuid WHERE id = $1`,
			r.movID, TxnKindRevaluation, txnID); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	return nil
}

func scanBackfillRevalRows(rows *sql.Rows) ([]backfillRevalRow, error) {
	defer rows.Close()
	out := make([]backfillRevalRow, 0)
	for rows.Next() {
		var r backfillRevalRow
		if err := rows.Scan(&r.movID, &r.item, &r.wh, &r.newCost, &r.note, &r.createdBy, &r.txnDate); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return out, nil
}

type openingGroupKey struct {
	date      string
	createdBy string
}

func backfillOpeningHeaders(ctx context.Context, sch appdb.SchemaSQL, tx *sql.Tx) error {
	rows, err := qquery(ctx, sch, tx, `
		SELECT id::text, catalog_item_id::text, warehouse_id::text, qty, unit_cost,
		       COALESCE(batch_no,''), expiry_date, COALESCE(created_by::text,''), created_at::date, created_at
		FROM inv_stock_movement m
		WHERE m.ref_id IS NULL AND m.movement_type = 'opening_balance'
		ORDER BY m.created_at`)
	if err != nil {
		return appErrs.Internal(err.Error())
	}

	type openingRow struct {
		movID, item, wh, batch, createdBy, txnDate string
		expiry                                     sql.NullTime
		qty, unitCost                              float64
		createdAt                                  time.Time
	}
	groups := map[openingGroupKey][]openingRow{}
	order := make([]openingGroupKey, 0)
	for rows.Next() {
		var r openingRow
		if err := rows.Scan(&r.movID, &r.item, &r.wh, &r.qty, &r.unitCost, &r.batch, &r.expiry,
			&r.createdBy, &r.txnDate, &r.createdAt); err != nil {
			rows.Close()
			return appErrs.Internal(err.Error())
		}
		key := openingGroupKey{date: r.txnDate, createdBy: r.createdBy}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], r)
	}
	if err := rows.Close(); err != nil {
		return appErrs.Internal(err.Error())
	}
	if err := rows.Err(); err != nil {
		return appErrs.Internal(err.Error())
	}

	for _, key := range order {
		lines := groups[key]
		if len(lines) == 0 {
			continue
		}
		txnID, docNo, err := insertStockTransactionHeader(ctx, sch, tx, TxnKindOpeningBalance, key.date, "Saldo awal (backfill)", key.createdBy)
		if err != nil {
			return err
		}
		for i, r := range lines {
			var lineID string
			if err := qrow(ctx, sch, tx, `
				INSERT INTO inv_stock_transaction_line
				  (transaction_id, catalog_item_id, warehouse_id, qty, unit_cost, batch_no, expiry_date, movement_id, sort_order)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8::uuid,$9)
				RETURNING id`,
				txnID, r.item, r.wh, round4(r.qty), round4(r.unitCost),
				nullStr(r.batch), r.expiry, r.movID, i).Scan(&lineID); err != nil {
				return appErrs.Internal(err.Error())
			}
			if _, err := qexec(ctx, sch, tx,
				`UPDATE inv_stock_movement SET ref_type = $2, ref_id = $3::uuid WHERE id = $1`,
				r.movID, TxnKindOpeningBalance, txnID); err != nil {
				return appErrs.Internal(err.Error())
			}
			_ = lineID
		}
		_ = docNo
	}
	return nil
}

type backfillTransferRow struct {
	outID, item, fromWh, toWh, note, createdBy, txnDate string
	qty                                                 float64
}

func backfillTransferHeaders(ctx context.Context, sch appdb.SchemaSQL, tx *sql.Tx) error {
	rows, err := qquery(ctx, sch, tx, `
		SELECT out_m.id::text, out_m.catalog_item_id::text, out_m.warehouse_id::text,
		       in_m.warehouse_id::text, out_m.qty,
		       COALESCE(out_m.note,''), COALESCE(out_m.created_by::text,''), out_m.created_at::date
		FROM inv_stock_movement out_m
		JOIN inv_stock_movement in_m ON in_m.source_movement_id = out_m.id
		WHERE out_m.ref_id IS NULL
		  AND out_m.movement_type = 'transfer_out'
		  AND in_m.movement_type = 'transfer_in'
		ORDER BY out_m.created_at`)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	items, err := scanBackfillTransferRows(rows)
	if err != nil {
		return err
	}
	for _, r := range items {
		txnID, _, err := insertStockTransactionHeader(ctx, sch, tx, TxnKindTransfer, r.txnDate, r.note, r.createdBy)
		if err != nil {
			return err
		}
		if _, err := qexec(ctx, sch, tx, `
			UPDATE inv_stock_transaction
			SET catalog_item_id = $2, from_warehouse_id = $3, to_warehouse_id = $4, signed_qty = $5, note = $6
			WHERE id = $1`,
			txnID, r.item, r.fromWh, r.toWh, r.qty, nullStr(r.note)); err != nil {
			return appErrs.Internal(err.Error())
		}
		if _, err := qexec(ctx, sch, tx, `
			UPDATE inv_stock_movement SET ref_type = $1, ref_id = $2::uuid
			WHERE ref_id IS NULL AND movement_type IN ('transfer_out','transfer_in')
			  AND (id = $3::uuid OR source_movement_id = $3::uuid)`,
			TxnKindTransfer, txnID, r.outID); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	return nil
}

func scanBackfillTransferRows(rows *sql.Rows) ([]backfillTransferRow, error) {
	defer rows.Close()
	out := make([]backfillTransferRow, 0)
	for rows.Next() {
		var r backfillTransferRow
		if err := rows.Scan(&r.outID, &r.item, &r.fromWh, &r.toWh, &r.qty, &r.note, &r.createdBy, &r.txnDate); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return out, nil
}
