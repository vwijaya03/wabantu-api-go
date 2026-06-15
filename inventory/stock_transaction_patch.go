package inventory

import (
	"context"

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

	existing, err := loadStockTransaction(ctx, conn, id)
	if err != nil {
		return nil, err
	}
	kind := existing.Kind
	if err := DeleteStockTransaction(ctx, id); err != nil {
		return nil, err
	}

	switch kind {
	case TxnKindAdjustment:
		qty := p.Qty
		if qty == 0 && existing.SignedQty != nil {
			qty = *existing.SignedQty
		}
		cat := coalesceStr(p.CatalogItemID, existing.CatalogItemID)
		wh := coalesceStr(p.WarehouseID, existing.WarehouseID)
		uc := p.UnitCost
		if uc == 0 && existing.UnitCost != nil {
			uc = *existing.UnitCost
		}
		if _, err := CreateAdjustment(ctx, &AdjustmentParams{
			CatalogItemID: cat, WarehouseID: wh, Qty: qty, UnitCost: uc,
			BatchNo: p.BatchNo, ExpiryDate: p.ExpiryDate, Note: coalesceStr(p.Note, existing.Note),
		}); err != nil {
			return nil, err
		}
	case TxnKindTransfer:
		qty := p.TransferQty
		if qty <= 0 && existing.SignedQty != nil {
			qty = *existing.SignedQty
		}
		if _, err := CreateTransfer(ctx, &TransferParams{
			CatalogItemID: coalesceStr(p.CatalogItemID, existing.CatalogItemID),
			FromWarehouseID: coalesceStr(p.FromWarehouseID, existing.FromWarehouseID),
			ToWarehouseID:   coalesceStr(p.ToWarehouseID, existing.ToWarehouseID),
			Qty: qty, Note: coalesceStr(p.Note, existing.Note),
		}); err != nil {
			return nil, err
		}
	case TxnKindOpeningBalance:
		entries := p.Entries
		if len(entries) == 0 {
			return nil, appErrs.BadRequest("entries wajib untuk edit saldo awal")
		}
		if _, err := CreateOpeningBalance(ctx, &OpeningBalanceParams{Entries: entries}); err != nil {
			return nil, err
		}
	case TxnKindRevaluation:
		nuc := p.NewUnitCost
		if nuc == 0 && existing.NewUnitCost != nil {
			nuc = *existing.NewUnitCost
		}
		if _, err := CreateRevaluation(ctx, &RevaluationParams{
			CatalogItemID: coalesceStr(p.CatalogItemID, existing.CatalogItemID),
			WarehouseID:   coalesceStr(p.WarehouseID, existing.WarehouseID),
			NewUnitCost:   nuc,
			Note:          coalesceStr(p.Note, existing.Note),
		}); err != nil {
			return nil, err
		}
	default:
		return nil, appErrs.BadRequest("jenis tidak didukung")
	}

	var newID string
	if err := conn.QueryRowContext(ctx,
		`SELECT id FROM inv_stock_transaction ORDER BY created_at DESC LIMIT 1`).Scan(&newID); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return loadStockTransaction(ctx, conn, newID)
}

func coalesceStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
