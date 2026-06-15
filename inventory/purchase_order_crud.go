package inventory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"encore.app/wabantu/finance"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

type UpdatePurchaseOrderParams struct {
	SupplierName    string                   `json:"supplierName"`
	ContactID       string                   `json:"contactId"`
	WarehouseID     string                   `json:"warehouseId"`
	TransactionDate string                   `json:"transactionDate"`
	Note            string                   `json:"note"`
	Lines           []PurchaseOrderLineInput `json:"lines"`
}

//encore:api auth method=DELETE path=/api/v1/inventory/purchase-orders/:id
func DeletePurchaseOrder(ctx context.Context, id string) error {
	u, err := getUser()
	if err != nil {
		return err
	}
	if err := requireOwner(u); err != nil {
		return err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer conn.Close()

	po, err := getPurchaseOrder(ctx, conn, id)
	if err != nil {
		return err
	}
	if err := finance.CheckPeriodUnlockedForDate(ctx, u.TenantSchema, po.TransactionDate); err != nil {
		return err
	}
	for _, l := range po.Lines {
		if l.QtyReceived > epsilon {
			return appErrs.BadRequest("PO sudah ada penerimaan, tidak bisa dihapus")
		}
	}
	var billCount int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pur_bill WHERE purchase_order_id = $1`, id).Scan(&billCount); err != nil {
		return appErrs.Internal(err.Error())
	}
	if billCount > 0 {
		return appErrs.BadRequest("PO sudah punya bill penerimaan, tidak bisa dihapus")
	}
	if po.Status != "open" && po.Status != "cancelled" {
		return appErrs.BadRequest("hanya PO terbuka atau dibatalkan yang bisa dihapus")
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM pur_purchase_order WHERE id = $1`, id); err != nil {
		return appErrs.Internal(err.Error())
	}
	return nil
}

//encore:api auth method=PATCH path=/api/v1/inventory/purchase-orders/:id
func UpdatePurchaseOrder(ctx context.Context, id string, p *UpdatePurchaseOrderParams) (*PurchaseOrder, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if len(p.Lines) == 0 {
		return nil, appErrs.BadRequest("minimal 1 baris item")
	}

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	po, err := getPurchaseOrder(ctx, conn, id)
	if err != nil {
		return nil, err
	}
	if err := finance.CheckPeriodUnlockedForDate(ctx, u.TenantSchema, po.TransactionDate); err != nil {
		return nil, err
	}
	if po.Status != "open" {
		return nil, appErrs.BadRequest("hanya PO berstatus open yang bisa diedit")
	}
	for _, l := range po.Lines {
		if l.QtyReceived > epsilon {
			return nil, appErrs.BadRequest("PO sudah ada penerimaan, tidak bisa diedit")
		}
	}

	txnDate := strings.TrimSpace(p.TransactionDate)
	if txnDate == "" {
		txnDate = po.TransactionDate
	} else if _, perr := time.Parse("2006-01-02", txnDate); perr != nil {
		return nil, appErrs.BadRequest("format tanggal harus YYYY-MM-DD")
	}
	defaultWarehouse := strings.TrimSpace(p.WarehouseID)
	if defaultWarehouse == "" && po.WarehouseID != nil {
		defaultWarehouse = *po.WarehouseID
	}
	var subtotal float64
	for i := range p.Lines {
		l := &p.Lines[i]
		if l.QtyOrdered <= epsilon {
			return nil, appErrs.BadRequest(fmt.Sprintf("baris %d: qty harus lebih dari 0", i+1))
		}
		if l.UnitCost < 0 {
			return nil, appErrs.BadRequest(fmt.Sprintf("baris %d: harga tidak boleh negatif", i+1))
		}
		if strings.TrimSpace(l.WarehouseID) == "" {
			l.WarehouseID = defaultWarehouse
		}
		if err := validateCatalogItem(ctx, conn, l.CatalogItemID); err != nil {
			return nil, fmt.Errorf("baris %d: %w", i+1, err)
		}
		if err := validateWarehouse(ctx, conn, l.WarehouseID); err != nil {
			return nil, fmt.Errorf("baris %d: %w", i+1, err)
		}
		subtotal += l.QtyOrdered * l.UnitCost
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM pur_purchase_order_line WHERE purchase_order_id = $1`, id); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE pur_purchase_order
		SET supplier_name = $2, contact_id = $3, warehouse_id = $4, transaction_date = $5,
		    note = $6, subtotal = $7, updated_at = now()
		WHERE id = $1`,
		id, nullStr(p.SupplierName), nullUUID(p.ContactID), nullUUID(defaultWarehouse),
		txnDate, nullStr(p.Note), round4(subtotal)); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	for _, l := range p.Lines {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO pur_purchase_order_line
			  (purchase_order_id, catalog_item_id, warehouse_id, description, qty_ordered, unit_cost)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			id, l.CatalogItemID, l.WarehouseID, nullStr(l.Description), round4(l.QtyOrdered), round4(l.UnitCost)); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return getPurchaseOrder(ctx, conn, id)
}
