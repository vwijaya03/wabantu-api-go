package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"encore.app/wabantu/finance"
	appdb "encore.app/wabantu/shared/db"
	appErrs "encore.app/wabantu/shared/errs"
)

// repostAdjustmentOnTx re-posts an adjustment against an existing transaction header.
func repostAdjustmentOnTx(ctx context.Context, sch appdb.SchemaSQL, tx *sql.Tx, txnID, docNo, accountID string, p *AdjustmentParams) (*movementPostResult, error) {
	mtype, dir, qty, ok := adjustmentPlan(p.Qty)
	if !ok {
		return nil, appErrs.BadRequest("qty penyesuaian tidak boleh nol")
	}
	expiry, perr := parseDatePtr(p.ExpiryDate)
	if perr != nil {
		return nil, perr
	}
	if err := validateCatalogItem(ctx, sch, tx, p.CatalogItemID); err != nil {
		return nil, err
	}
	if err := validateWarehouse(ctx, sch, tx, p.WarehouseID); err != nil {
		return nil, err
	}
	if err := ensureSku(ctx, sch, tx, p.CatalogItemID); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if _, err := qexec(ctx, sch, tx, `
		UPDATE inv_stock_transaction
		SET catalog_item_id = $2, warehouse_id = $3, signed_qty = $4, unit_cost = $5, note = $6, updated_at = now()
		WHERE id = $1`,
		txnID, p.CatalogItemID, p.WarehouseID, p.Qty, nullFloat(p.UnitCost), nullStr(p.Note)); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	cc, err := loadCostingContext(ctx, sch, tx, p.CatalogItemID)
	if err != nil {
		return nil, err
	}
	res, err := PostMovement(ctx, sch, tx, MovementInput{
		CatalogItemID: p.CatalogItemID,
		WarehouseID:   p.WarehouseID,
		Type:          mtype,
		Direction:     dir,
		Qty:           qty,
		UnitCost:      p.UnitCost,
		CostingMethod: cc.method,
		BlockNegative: cc.blockNegative,
		BatchNo:       p.BatchNo,
		ExpiryDate:    expiry,
		RefType:       TxnKindAdjustment,
		RefID:         txnID,
		Note:          p.Note,
		CreatedBy:     accountID,
	})
	if err != nil {
		return nil, err
	}
	return &movementPostResult{res: *res, dir: dir, note: p.Note}, nil
}

type movementPostResult struct {
	res  MovementResult
	dir  string
	note string
}

func repostTransferOnTx(ctx context.Context, sch appdb.SchemaSQL, tx *sql.Tx, txnID, accountID string, p *TransferParams) error {
	if p.Qty <= epsilon {
		return appErrs.BadRequest("qty transfer harus lebih dari 0")
	}
	if strings.TrimSpace(p.FromWarehouseID) == strings.TrimSpace(p.ToWarehouseID) {
		return appErrs.BadRequest("gudang asal dan tujuan tidak boleh sama")
	}
	if err := validateCatalogItem(ctx, sch, tx, p.CatalogItemID); err != nil {
		return err
	}
	if err := validateWarehouse(ctx, sch, tx, p.FromWarehouseID); err != nil {
		return err
	}
	if err := validateWarehouse(ctx, sch, tx, p.ToWarehouseID); err != nil {
		return err
	}
	if err := ensureSku(ctx, sch, tx, p.CatalogItemID); err != nil {
		return appErrs.Internal(err.Error())
	}
	if _, err := qexec(ctx, sch, tx, `
		UPDATE inv_stock_transaction
		SET catalog_item_id = $2, from_warehouse_id = $3, to_warehouse_id = $4, signed_qty = $5, note = $6, updated_at = now()
		WHERE id = $1`,
		txnID, p.CatalogItemID, p.FromWarehouseID, p.ToWarehouseID, p.Qty, nullStr(p.Note)); err != nil {
		return appErrs.Internal(err.Error())
	}
	cc, err := loadCostingContext(ctx, sch, tx, p.CatalogItemID)
	if err != nil {
		return err
	}
	out, err := PostMovement(ctx, sch, tx, MovementInput{
		CatalogItemID: p.CatalogItemID,
		WarehouseID:   p.FromWarehouseID,
		Type:          MovementTransferOut,
		Direction:     dirOut,
		Qty:           round4(p.Qty),
		CostingMethod: cc.method,
		BlockNegative: cc.blockNegative,
		RefType:       TxnKindTransfer,
		RefID:         txnID,
		Note:          p.Note,
		CreatedBy:     accountID,
	})
	if err != nil {
		return err
	}
	_, err = PostMovement(ctx, sch, tx, MovementInput{
		CatalogItemID:    p.CatalogItemID,
		WarehouseID:      p.ToWarehouseID,
		Type:             MovementTransferIn,
		Direction:        dirIn,
		Qty:              round4(p.Qty),
		UnitCost:         out.UnitCost,
		CostingMethod:    cc.method,
		BlockNegative:    cc.blockNegative,
		RefType:          TxnKindTransfer,
		RefID:            txnID,
		SourceMovementID: out.MovementID,
		Note:             p.Note,
		CreatedBy:        accountID,
	})
	return err
}

func repostOpeningOnTx(ctx context.Context, sch appdb.SchemaSQL, tx *sql.Tx, txnID, docNo, accountID string, entries []OpeningEntry) error {
	for i, e := range entries {
		if e.Qty <= epsilon {
			return appErrs.BadRequest(fmt.Sprintf("baris %d: qty harus lebih dari 0", i+1))
		}
		if e.UnitCost < 0 {
			return appErrs.BadRequest(fmt.Sprintf("baris %d: harga pokok tidak boleh negatif", i+1))
		}
		if err := validateCatalogItem(ctx, sch, tx, e.CatalogItemID); err != nil {
			return fmt.Errorf("baris %d: %w", i+1, err)
		}
		if err := validateWarehouse(ctx, sch, tx, e.WarehouseID); err != nil {
			return fmt.Errorf("baris %d: %w", i+1, err)
		}
		expiry, perr := parseDatePtr(e.ExpiryDate)
		if perr != nil {
			return fmt.Errorf("baris %d: %w", i+1, perr)
		}
		if err := ensureSku(ctx, sch, tx, e.CatalogItemID); err != nil {
			return appErrs.Internal(err.Error())
		}
		cc, cerr := loadCostingContext(ctx, sch, tx, e.CatalogItemID)
		if cerr != nil {
			return cerr
		}
		var lineID string
		if err := qrow(ctx, sch, tx, `
			INSERT INTO inv_stock_transaction_line
			  (transaction_id, catalog_item_id, warehouse_id, qty, unit_cost, batch_no, expiry_date, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			RETURNING id`,
			txnID, e.CatalogItemID, e.WarehouseID, round4(e.Qty), round4(e.UnitCost),
			nullStr(e.BatchNo), nullTime(expiry), i).Scan(&lineID); err != nil {
			return appErrs.Internal(err.Error())
		}
		res, merr := PostMovement(ctx, sch, tx, MovementInput{
			CatalogItemID: e.CatalogItemID,
			WarehouseID:   e.WarehouseID,
			Type:          MovementOpening,
			Direction:     dirIn,
			Qty:           round4(e.Qty),
			UnitCost:      e.UnitCost,
			CostingMethod: cc.method,
			BlockNegative: cc.blockNegative,
			BatchNo:       e.BatchNo,
			ExpiryDate:    expiry,
			RefType:       TxnKindOpeningBalance,
			RefID:         txnID,
			RefLineID:     lineID,
			Note:          "Saldo awal " + docNo,
			CreatedBy:     accountID,
		})
		if merr != nil {
			return merr
		}
		if _, err := qexec(ctx, sch, tx,
			`UPDATE inv_stock_transaction_line SET movement_id = $2 WHERE id = $1`, lineID, res.MovementID); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	return nil
}

func repostRevaluationOnTx(ctx context.Context, sch appdb.SchemaSQL, tx *sql.Tx, txnID, accountID, tenantSchema string, p *RevaluationParams) (movementID string, delta float64, err error) {
	if p.NewUnitCost < 0 {
		return "", 0, appErrs.BadRequest("harga pokok baru tidak boleh negatif")
	}
	if err := validateCatalogItem(ctx, sch, tx, p.CatalogItemID); err != nil {
		return "", 0, err
	}
	if err := validateWarehouse(ctx, sch, tx, p.WarehouseID); err != nil {
		return "", 0, err
	}
	var onHand, oldTotal float64
	err = qrow(ctx, sch, tx, `
		SELECT on_hand, total_value FROM inv_stock_balance
		WHERE catalog_item_id = $1 AND warehouse_id = $2 FOR UPDATE`,
		p.CatalogItemID, p.WarehouseID).Scan(&onHand, &oldTotal)
	if errors.Is(err, sql.ErrNoRows) || onHand <= epsilon {
		return "", 0, appErrs.BadRequest("tidak ada stok untuk direvaluasi di gudang ini")
	}
	if err != nil {
		return "", 0, appErrs.Internal(err.Error())
	}
	if _, err := qexec(ctx, sch, tx, `
		UPDATE inv_stock_transaction
		SET catalog_item_id = $2, warehouse_id = $3, new_unit_cost = $4, note = $5, updated_at = now()
		WHERE id = $1`,
		txnID, p.CatalogItemID, p.WarehouseID, p.NewUnitCost, nullStr(p.Note)); err != nil {
		return "", 0, appErrs.Internal(err.Error())
	}
	newTotal, delta := revaluationDelta(onHand, oldTotal, p.NewUnitCost)
	cc, err := loadCostingContext(ctx, sch, tx, p.CatalogItemID)
	if err != nil {
		return "", 0, err
	}
	if cc.method != CostingAverage {
		if oldTotal > epsilon {
			factor := newTotal / oldTotal
			if _, err := qexec(ctx, sch, tx, `
				UPDATE inv_cost_layer SET unit_cost = ROUND(unit_cost * $3, 4)
				WHERE catalog_item_id = $1 AND warehouse_id = $2 AND qty_remaining > 0`,
				p.CatalogItemID, p.WarehouseID, factor); err != nil {
				return "", 0, appErrs.Internal(err.Error())
			}
		} else {
			if _, err := qexec(ctx, sch, tx, `
				UPDATE inv_cost_layer SET unit_cost = $3
				WHERE catalog_item_id = $1 AND warehouse_id = $2 AND qty_remaining > 0`,
				p.CatalogItemID, p.WarehouseID, p.NewUnitCost); err != nil {
				return "", 0, appErrs.Internal(err.Error())
			}
		}
	}
	if _, err := qexec(ctx, sch, tx, `
		UPDATE inv_stock_balance SET avg_unit_cost = $3, total_value = $4, updated_at = now()
		WHERE catalog_item_id = $1 AND warehouse_id = $2`,
		p.CatalogItemID, p.WarehouseID, p.NewUnitCost, newTotal); err != nil {
		return "", 0, appErrs.Internal(err.Error())
	}
	dir := dirIn
	if delta < 0 {
		dir = dirOut
	}
	if err := qrow(ctx, sch, tx, `
		INSERT INTO inv_stock_movement
		  (catalog_item_id, warehouse_id, movement_type, direction, qty, unit_cost, total_cost,
		   qty_after, avg_cost_after, ref_type, ref_id, note, created_by)
		VALUES ($1,$2,$3,$4,0,$5,$6,$7,$5,$8,$9,$10,$11)
		RETURNING id`,
		p.CatalogItemID, p.WarehouseID, MovementRevaluation, dir, p.NewUnitCost,
		round4(abs(delta)), onHand, TxnKindRevaluation, txnID, nullStr(p.Note), nullUUID(accountID)).Scan(&movementID); err != nil {
		return "", 0, appErrs.Internal(err.Error())
	}
	_ = tenantSchema
	return movementID, delta, nil
}

func recordAdjustmentFinance(ctx context.Context, tenantSchema, accountID string, r *movementPostResult) error {
	if r == nil || r.dir != dirOut || r.res.TotalCost <= 0 {
		return nil
	}
	desc := fmt.Sprintf("Penyesuaian stok keluar (%s)", strings.TrimSpace(r.note))
	return finance.RecordInventoryEntry(ctx, tenantSchema, accountID,
		r.res.MovementID, "expense", finCatSelisihPersediaan, desc, round2(r.res.TotalCost), "")
}

func recordRevaluationFinance(ctx context.Context, tenantSchema, accountID, movementID, note string, delta float64) error {
	if abs(delta) <= 0 {
		return nil
	}
	flow := "income"
	if delta < 0 {
		flow = "expense"
	}
	desc := fmt.Sprintf("Revaluasi HPP (%s)", strings.TrimSpace(note))
	return finance.RecordInventoryEntry(ctx, tenantSchema, accountID,
		movementID, flow, finCatPenyesuaianNilai, desc, round2(abs(delta)), "")
}
