package inventory

import (
	"context"

	"encore.app/wabantu/finance"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

type UpdateStockTransactionParams struct {
	CatalogItemID   string         `json:"catalogItemId"`
	WarehouseID     string         `json:"warehouseId"`
	Qty             float64        `json:"qty"`
	UnitCost        float64        `json:"unitCost"`
	BatchNo         string         `json:"batchNo"`
	ExpiryDate      *string        `json:"expiryDate"`
	Note            string         `json:"note"`
	FromWarehouseID string         `json:"fromWarehouseId"`
	ToWarehouseID   string         `json:"toWarehouseId"`
	TransferQty     float64        `json:"transferQty"`
	NewUnitCost     float64        `json:"newUnitCost"`
	Entries         []OpeningEntry `json:"entries"`
}

//encore:api auth method=PATCH path=/api/v1/inventory/stock-transactions/:id
func UpdateStockTransaction(ctx context.Context, id string, p *UpdateStockTransactionParams) (*StockTransaction, error) {
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
	defer conn.Close()
	if err := ensureInventoryModuleReady(ctx, conn); err != nil {
		return nil, err
	}

	existing, err := loadStockTransaction(ctx, conn, id)
	if err != nil {
		return nil, err
	}
	if err := financeCheckPeriodForTxn(ctx, u.TenantSchema, existing.TransactionDate); err != nil {
		return nil, err
	}

	movs, err := collectMovementsByRef(ctx, conn, txnKindRefType(existing.Kind), id)
	if err != nil {
		return nil, err
	}
	if err := removeFinanceForMovements(ctx, u.TenantSchema, movs); err != nil {
		return nil, err
	}

	tx, terr := conn.BeginTx(ctx, nil)
	if terr != nil {
		return nil, appErrs.Internal(terr.Error())
	}
	defer tx.Rollback()

	if _, err := purgeMovementsByRef(ctx, tx, txnKindRefType(existing.Kind), id); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM inv_stock_transaction_line WHERE transaction_id = $1`, id); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	var adjResult *movementPostResult
	var revalMovID string
	var revalDelta float64
	var revalNote string

	switch existing.Kind {
	case TxnKindAdjustment:
		qty := p.Qty
		if qty == 0 && existing.SignedQty != nil {
			qty = *existing.SignedQty
		}
		uc := p.UnitCost
		if uc == 0 && existing.UnitCost != nil {
			uc = *existing.UnitCost
		}
		adjResult, err = repostAdjustmentOnTx(ctx, tx, id, existing.DocNo, u.AccountID, &AdjustmentParams{
			CatalogItemID: coalesceStr(p.CatalogItemID, existing.CatalogItemID),
			WarehouseID:   coalesceStr(p.WarehouseID, existing.WarehouseID),
			Qty:           qty,
			UnitCost:      uc,
			BatchNo:       p.BatchNo,
			ExpiryDate:    p.ExpiryDate,
			Note:          coalesceStr(p.Note, existing.Note),
		})
	case TxnKindTransfer:
		qty := p.TransferQty
		if qty <= 0 && existing.SignedQty != nil {
			qty = *existing.SignedQty
		}
		err = repostTransferOnTx(ctx, tx, id, u.AccountID, &TransferParams{
			CatalogItemID:   coalesceStr(p.CatalogItemID, existing.CatalogItemID),
			FromWarehouseID: coalesceStr(p.FromWarehouseID, existing.FromWarehouseID),
			ToWarehouseID:   coalesceStr(p.ToWarehouseID, existing.ToWarehouseID),
			Qty:             qty,
			Note:            coalesceStr(p.Note, existing.Note),
		})
	case TxnKindOpeningBalance:
		entries := p.Entries
		if len(entries) == 0 {
			return nil, appErrs.BadRequest("entries wajib untuk edit saldo awal")
		}
		err = repostOpeningOnTx(ctx, tx, id, existing.DocNo, u.AccountID, entries)
	case TxnKindRevaluation:
		nuc := p.NewUnitCost
		if nuc == 0 && existing.NewUnitCost != nil {
			nuc = *existing.NewUnitCost
		}
		revalNote = coalesceStr(p.Note, existing.Note)
		revalMovID, revalDelta, err = repostRevaluationOnTx(ctx, tx, id, u.AccountID, u.TenantSchema, &RevaluationParams{
			CatalogItemID: coalesceStr(p.CatalogItemID, existing.CatalogItemID),
			WarehouseID:   coalesceStr(p.WarehouseID, existing.WarehouseID),
			NewUnitCost:   nuc,
			Note:          revalNote,
		})
	default:
		return nil, appErrs.BadRequest("jenis tidak didukung")
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	if err := recordAdjustmentFinance(ctx, u.TenantSchema, u.AccountID, adjResult); err != nil {
		return nil, err
	}
	if err := recordRevaluationFinance(ctx, u.TenantSchema, u.AccountID, revalMovID, revalNote, revalDelta); err != nil {
		return nil, err
	}
	return loadStockTransaction(ctx, conn, id)
}

func financeCheckPeriodForTxn(ctx context.Context, schema, txnDate string) error {
	return finance.CheckPeriodUnlockedForDate(ctx, schema, txnDate)
}

func removeFinanceForMovements(ctx context.Context, schema string, movs []movementRef) error {
	for _, m := range movs {
		if err := finance.RemoveInventoryEntry(ctx, schema, m.id); err != nil {
			return err
		}
	}
	return nil
}

func coalesceStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
