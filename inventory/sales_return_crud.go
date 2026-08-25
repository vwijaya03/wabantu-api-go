package inventory

import (
	appdb "encore.app/wabantu/shared/db"
	"context"
	"fmt"
	"strings"

	"encore.app/wabantu/finance"
	appErrs "encore.app/wabantu/shared/errs"
)

//encore:api auth method=DELETE path=/api/v1/inventory/sales-returns/:id
func DeleteSalesReturn(ctx context.Context, id string) error {
	u, err := getUser()
	if err != nil {
		return err
	}
	if err := requireOwner(u); err != nil {
		return err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return err
	}
	pool := tenantDB()
	return deleteSalesReturnConn(ctx, sch, pool, u.TenantSchema, id)
}

func deleteSalesReturnConn(ctx context.Context, sch appdb.SchemaSQL, q querier, tenantSchema, id string) error {
	ret, err := getSalesReturn(ctx, sch, q, id)
	if err != nil {
		return err
	}
	if err := finance.CheckPeriodUnlockedForDate(ctx, tenantSchema, ret.TransactionDate); err != nil {
		return err
	}
	movs, err := collectMovementsByRef(ctx, sch, q, "sales_return", id)
	if err != nil {
		return err
	}
	if err := finance.RemoveInventoryEntries(ctx, tenantSchema, movementFinanceRefs("ret:"+id, movs)); err != nil {
		return err
	}
	pool := tenantDB()
	tx, terr := pool.BeginTx(ctx, nil)
	if terr != nil {
		return appErrs.Internal(terr.Error())
	}
	defer tx.Rollback()
	if _, err := purgeMovementsByRef(ctx, sch, tx, "sales_return", id); err != nil {
		return err
	}
	if _, err := qexec(ctx, sch, tx, `DELETE FROM inv_sales_return WHERE id = $1`, id); err != nil {
		return appErrs.Internal(err.Error())
	}
	return tx.Commit()
}

type UpdateSalesReturnParams struct {
	Note  string                 `json:"note"`
	Lines []SalesReturnLineInput `json:"lines"`
}

//encore:api auth method=PATCH path=/api/v1/inventory/sales-returns/:id
func UpdateSalesReturn(ctx context.Context, id string, p *UpdateSalesReturnParams) (*SalesReturn, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if len(p.Lines) == 0 {
		return nil, appErrs.BadRequest("minimal 1 baris retur")
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	existing, err := getSalesReturn(ctx, sch, pool, id)
	if err != nil {
		return nil, err
	}
	if existing.OrderID == nil || *existing.OrderID == "" {
		return nil, appErrs.BadRequest("retur tidak punya orderId")
	}
	orderID := *existing.OrderID
	if err := finance.CheckPeriodUnlockedForDate(ctx, u.TenantSchema, existing.TransactionDate); err != nil {
		return nil, err
	}

	contactID, _, _, _, err := loadOrderForInvoice(ctx, sch, pool, orderID)
	if err != nil {
		return nil, err
	}
	costByItem, err := orderItemSaleCost(ctx, sch, pool, orderID)
	if err != nil {
		return nil, err
	}
	alreadyReturned, err := orderReturnedQty(ctx, sch, pool, orderID)
	if err != nil {
		return nil, err
	}
	// Exclude this return from already-returned totals during validation.
	for _, l := range existing.Lines {
		alreadyReturned[l.CatalogItemID] -= l.Qty
		if alreadyReturned[l.CatalogItemID] < 0 {
			alreadyReturned[l.CatalogItemID] = 0
		}
	}
	for i, l := range p.Lines {
		if l.Qty <= epsilon {
			return nil, appErrs.BadRequest(fmt.Sprintf("baris %d: qty harus lebih dari 0", i+1))
		}
		sold := costByItem[l.CatalogItemID].qty
		if returnableQty(sold, alreadyReturned[l.CatalogItemID]) < l.Qty-epsilon {
			return nil, appErrs.BadRequest(fmt.Sprintf("baris %d: qty retur melebihi yang terjual (sisa %g)",
				i+1, returnableQty(sold, alreadyReturned[l.CatalogItemID])))
		}
	}

	movs, err := collectMovementsByRef(ctx, sch, pool, "sales_return", id)
	if err != nil {
		return nil, err
	}
	if err := finance.RemoveInventoryEntries(ctx, u.TenantSchema, movementFinanceRefs("ret:"+id, movs)); err != nil {
		return nil, err
	}

	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	if _, err := purgeMovementsByRef(ctx, sch, tx, "sales_return", id); err != nil {
		return nil, err
	}
	if _, err := qexec(ctx, sch, tx, `DELETE FROM inv_sales_return_line WHERE sales_return_id = $1`, id); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if _, err := qexec(ctx, sch, tx,
		`UPDATE inv_sales_return SET note = $2, total_cost = 0, updated_at = now() WHERE id = $1`,
		id, nullStr(p.Note)); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	var totalCost float64
	returnNo := existing.ReturnNo
	for _, l := range p.Lines {
		wh := strings.TrimSpace(l.WarehouseID)
		if wh == "" {
			wh, _ = defaultWarehouseID(ctx, sch, pool)
		}
		unitCost := weightedItemCost(costByItem, l.CatalogItemID)
		srcMovement := orderSaleMovementID(ctx, sch, pool, orderID, l.CatalogItemID)
		cc, cerr := loadCostingContext(ctx, sch, tx, l.CatalogItemID)
		if cerr != nil {
			return nil, cerr
		}
		res, merr := PostMovement(ctx, sch, tx, MovementInput{
			CatalogItemID: l.CatalogItemID, WarehouseID: wh,
			Type: MovementReturnIn, Direction: dirIn, Qty: round4(l.Qty),
			UnitCost: unitCost, CostingMethod: cc.method, BlockNegative: false,
			RefType: "sales_return", RefID: id, SourceMovementID: srcMovement,
			Note: "Retur penjualan " + returnNo, CreatedBy: u.AccountID,
		})
		if merr != nil {
			return nil, merr
		}
		lineCost := round4(unitCost * l.Qty)
		totalCost += lineCost
		if _, err := qexec(ctx, sch, tx, `
			INSERT INTO inv_sales_return_line
			  (sales_return_id, catalog_item_id, warehouse_id, qty, unit_cost, movement_id, source_movement_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			id, l.CatalogItemID, wh, round4(l.Qty), unitCost, res.MovementID, nullUUID(srcMovement)); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	if _, err := qexec(ctx, sch, tx,
		`UPDATE inv_sales_return SET total_cost=$2, contact_id=$3, updated_at=now() WHERE id=$1`,
		id, round4(totalCost), nullUUID(contactID)); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	_, postExpense, _, serr := loadSyncSetting(ctx, sch, pool)
	if serr == nil && !postExpense && totalCost > 0 {
		if err := finance.RecordInventoryEntry(ctx, u.TenantSchema, u.AccountID,
			"ret:"+id, "income", finCatHPP, "Retur penjualan "+returnNo, round2(totalCost), ""); err != nil {
			return nil, err
		}
	}
	return getSalesReturn(ctx, sch, pool, id)
}
