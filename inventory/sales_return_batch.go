package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"encore.app/wabantu/finance"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

type ReturnableLine struct {
	CatalogItemID  string  `json:"catalogItemId"`
	ItemName       string  `json:"itemName"`
	WarehouseID    string  `json:"warehouseId,omitempty"`
	QtySold        float64 `json:"qtySold"`
	QtyReturned    float64 `json:"qtyReturned"`
	QtyReturnable  float64 `json:"qtyReturnable"`
}

type ReturnableOrderLines struct {
	OrderID string           `json:"orderId"`
	Status  string           `json:"status"`
	Lines   []ReturnableLine `json:"lines"`
}

//encore:api auth method=GET path=/api/v1/inventory/return-eligible-lines/:orderID
func GetReturnableOrderLines(ctx context.Context, orderID string) (*ReturnableOrderLines, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	return loadReturnableOrderLines(ctx, conn, orderID)
}

func loadReturnableOrderLines(ctx context.Context, conn *sql.Conn, orderID string) (*ReturnableOrderLines, error) {
	_, lines, _, status, err := loadOrderForInvoice(ctx, conn, orderID)
	if err != nil {
		return nil, err
	}
	if !isInvoiceEligibleStatus(status) {
		return nil, appErrs.BadRequest("status pesanan harus Dalam pengiriman atau Selesai")
	}
	costByItem, err := orderItemSaleCost(ctx, conn, orderID)
	if err != nil {
		return nil, err
	}
	alreadyReturned, err := orderReturnedQty(ctx, conn, orderID)
	if err != nil {
		return nil, err
	}

	out := &ReturnableOrderLines{OrderID: orderID, Status: status}
	for _, l := range lines {
		if strings.TrimSpace(l.CatalogItemID) == "" {
			continue
		}
		sold := costByItem[l.CatalogItemID].qty
		ret := alreadyReturned[l.CatalogItemID]
		retable := returnableQty(sold, ret)
		if retable <= epsilon {
			continue
		}
		out.Lines = append(out.Lines, ReturnableLine{
			CatalogItemID: l.CatalogItemID,
			ItemName:      l.Name,
			WarehouseID:   l.WarehouseID,
			QtySold:       round4(sold),
			QtyReturned:   round4(ret),
			QtyReturnable: retable,
		})
	}
	return out, nil
}

type BatchSalesReturnOrderInput struct {
	OrderID string                 `json:"orderId"`
	Note    string                 `json:"note"`
	Lines   []SalesReturnLineInput `json:"lines"`
}

type BatchCreateSalesReturnsParams struct {
	Orders []BatchSalesReturnOrderInput `json:"orders"`
}

type BatchSalesReturnResultLine struct {
	OrderID  string `json:"orderId"`
	ReturnID string `json:"returnId,omitempty"`
	ReturnNo string `json:"returnNo,omitempty"`
	Error    string `json:"error,omitempty"`
}

type BatchCreateSalesReturnsResponse struct {
	Processed int                          `json:"processed"`
	Failed    int                          `json:"failed"`
	Results   []BatchSalesReturnResultLine `json:"results"`
}

//encore:api auth method=POST path=/api/v1/inventory/sales-return-batch
func BatchCreateSalesReturns(ctx context.Context, p *BatchCreateSalesReturnsParams) (*BatchCreateSalesReturnsResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if len(p.Orders) == 0 {
		return nil, appErrs.BadRequest("minimal 1 pesanan")
	}
	if len(p.Orders) > maxBatchOrderActions {
		return nil, appErrs.BadRequest(fmt.Sprintf("maksimal %d pesanan per aksi", maxBatchOrderActions))
	}

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	resp := &BatchCreateSalesReturnsResponse{
		Results: make([]BatchSalesReturnResultLine, 0, len(p.Orders)),
	}
	for _, o := range p.Orders {
		line := BatchSalesReturnResultLine{OrderID: strings.TrimSpace(o.OrderID)}
		if line.OrderID == "" {
			line.Error = "orderId kosong"
			resp.Failed++
			resp.Results = append(resp.Results, line)
			continue
		}
		if len(o.Lines) == 0 {
			line.Error = "tidak ada baris retur"
			resp.Failed++
			resp.Results = append(resp.Results, line)
			continue
		}
		ret, rerr := createSalesReturnConn(ctx, conn, u.TenantSchema, u.AccountID, &CreateSalesReturnParams{
			OrderID: line.OrderID,
			Note:    o.Note,
			Lines:   o.Lines,
		})
		if rerr != nil {
			line.Error = rerr.Error()
			resp.Failed++
		} else {
			line.ReturnID = ret.ID
			line.ReturnNo = ret.ReturnNo
			resp.Processed++
		}
		resp.Results = append(resp.Results, line)
	}
	return resp, nil
}

func createSalesReturnConn(ctx context.Context, conn *sql.Conn, tenantSchema, accountID string, p *CreateSalesReturnParams) (*SalesReturn, error) {
	if strings.TrimSpace(p.OrderID) == "" {
		return nil, appErrs.BadRequest("orderId wajib diisi")
	}
	if len(p.Lines) == 0 {
		return nil, appErrs.BadRequest("minimal 1 baris retur")
	}
	if err := finance.CheckCurrentPeriodUnlocked(ctx, tenantSchema); err != nil {
		return nil, err
	}

	_, _, _, status, err := loadOrderForInvoice(ctx, conn, p.OrderID)
	if err != nil {
		return nil, err
	}
	if !isInvoiceEligibleStatus(status) {
		return nil, appErrs.BadRequest("status pesanan harus Dalam pengiriman atau Selesai")
	}

	contactID, _, _, _, err := loadOrderForInvoice(ctx, conn, p.OrderID)
	if err != nil {
		return nil, err
	}
	costByItem, err := orderItemSaleCost(ctx, conn, p.OrderID)
	if err != nil {
		return nil, err
	}
	alreadyReturned, err := orderReturnedQty(ctx, conn, p.OrderID)
	if err != nil {
		return nil, err
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

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	returnNo, err := nextDocNumber(ctx, tx, DocReturn, DocReturn)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	var returnID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO inv_sales_return (return_no, order_id, contact_id, status, note, total_cost, created_by)
		VALUES ($1,$2,$3,'posted',$4,0,$5)
		RETURNING id`,
		returnNo, nullUUID(p.OrderID), nullUUID(contactID), nullStr(p.Note), nullUUID(accountID)).Scan(&returnID); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	var totalCost float64
	for _, l := range p.Lines {
		wh := strings.TrimSpace(l.WarehouseID)
		if wh == "" {
			wh, _ = defaultWarehouseID(ctx, conn)
		}
		unitCost := weightedItemCost(costByItem, l.CatalogItemID)
		srcMovement := orderSaleMovementID(ctx, conn, p.OrderID, l.CatalogItemID)
		cc, cerr := loadCostingContext(ctx, tx, l.CatalogItemID)
		if cerr != nil {
			return nil, cerr
		}
		res, merr := PostMovement(ctx, tx, MovementInput{
			CatalogItemID: l.CatalogItemID, WarehouseID: wh,
			Type: MovementReturnIn, Direction: dirIn, Qty: round4(l.Qty),
			UnitCost: unitCost, CostingMethod: cc.method, BlockNegative: false,
			RefType: "sales_return", RefID: returnID, SourceMovementID: srcMovement,
			Note: "Retur penjualan " + returnNo, CreatedBy: accountID,
		})
		if merr != nil {
			return nil, merr
		}
		lineCost := round4(unitCost * l.Qty)
		totalCost += lineCost
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inv_sales_return_line
			  (sales_return_id, catalog_item_id, warehouse_id, qty, unit_cost, movement_id, source_movement_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			returnID, l.CatalogItemID, wh, round4(l.Qty), unitCost, res.MovementID, nullUUID(srcMovement)); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE inv_sales_return SET total_cost=$2, updated_at=now() WHERE id=$1`, returnID, round4(totalCost)); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	_, postExpense, _, serr := loadSyncSetting(ctx, conn)
	if serr == nil && !postExpense && totalCost > 0 {
		if err := finance.RecordInventoryEntry(ctx, tenantSchema, accountID,
			"ret:"+returnID, "income", finCatHPP, "Retur penjualan "+returnNo, round2(totalCost)); err != nil {
			return nil, err
		}
	}
	return getSalesReturn(ctx, conn, returnID)
}
