package inventory

import (
	appdb "encore.app/wabantu/shared/db"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
)

// ---------- types ----------

type PurchaseOrderLine struct {
	ID            string  `json:"id"`
	CatalogItemID string  `json:"catalogItemId"`
	ItemName      string  `json:"itemName,omitempty"`
	WarehouseID   string  `json:"warehouseId"`
	Description   string  `json:"description,omitempty"`
	QtyOrdered    float64 `json:"qtyOrdered"`
	QtyReceived   float64 `json:"qtyReceived"`
	UnitCost      float64 `json:"unitCost"`
}

type PurchaseOrder struct {
	ID              string              `json:"id"`
	PONo            string              `json:"poNo"`
	SupplierName    string              `json:"supplierName,omitempty"`
	ContactID       *string             `json:"contactId,omitempty"`
	WarehouseID     *string             `json:"warehouseId,omitempty"`
	Status          string              `json:"status"`
	TransactionDate string              `json:"transactionDate"`
	Note            string              `json:"note,omitempty"`
	Subtotal        float64             `json:"subtotal"`
	Lines           []PurchaseOrderLine `json:"lines"`
	CreatedAt       time.Time           `json:"createdAt"`
}

type PurchaseOrderLineInput struct {
	CatalogItemID string  `json:"catalogItemId"`
	WarehouseID   string  `json:"warehouseId"`
	Description   string  `json:"description"`
	QtyOrdered    float64 `json:"qtyOrdered"`
	UnitCost      float64 `json:"unitCost"`
}

type CreatePurchaseOrderParams struct {
	SupplierName    string                   `json:"supplierName"`
	ContactID       string                   `json:"contactId"`
	WarehouseID     string                   `json:"warehouseId"`
	TransactionDate string                   `json:"transactionDate"`
	Note            string                   `json:"note"`
	Lines           []PurchaseOrderLineInput `json:"lines"`
}

type ListPurchaseOrdersParams struct {
	Status   string `query:"status"`
	Q        string `query:"q"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListPurchaseOrdersResponse struct {
	PurchaseOrders []PurchaseOrder `json:"purchaseOrders"`
	Total          int             `json:"total"`
	Page           int             `json:"page"`
	PageSize       int             `json:"pageSize"`
}

// ---------- endpoints ----------

//encore:api auth method=POST path=/api/v1/inventory/purchase-orders
func CreatePurchaseOrder(ctx context.Context, p *CreatePurchaseOrderParams) (*PurchaseOrder, error) {
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
	txnDate := strings.TrimSpace(p.TransactionDate)
	if txnDate == "" {
		txnDate = time.Now().Format("2006-01-02")
	} else if _, perr := time.Parse("2006-01-02", txnDate); perr != nil {
		return nil, appErrs.BadRequest("format tanggal harus YYYY-MM-DD")
	}

	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	defaultWarehouse := strings.TrimSpace(p.WarehouseID)
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
		if err := validateCatalogItem(ctx, sch, pool, l.CatalogItemID); err != nil {
			return nil, fmt.Errorf("baris %d: %w", i+1, err)
		}
		if err := validateWarehouse(ctx, sch, pool, l.WarehouseID); err != nil {
			return nil, fmt.Errorf("baris %d: %w", i+1, err)
		}
		subtotal += l.QtyOrdered * l.UnitCost
	}

	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	poNo, err := nextDocNumber(ctx, sch, tx, DocPO, DocPO)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	var poID string
	if err := qrow(ctx, sch, tx, `
		INSERT INTO pur_purchase_order
		  (po_no, supplier_name, contact_id, warehouse_id, status, transaction_date, note, subtotal, created_by)
		VALUES ($1,$2,$3,$4,'open',$5,$6,$7,$8)
		RETURNING id`,
		poNo, nullStr(p.SupplierName), nullUUID(p.ContactID), nullUUID(defaultWarehouse),
		txnDate, nullStr(p.Note), round4(subtotal), nullUUID(u.AccountID)).Scan(&poID); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	for _, l := range p.Lines {
		if _, err := qexec(ctx, sch, tx, `
			INSERT INTO pur_purchase_order_line
			  (purchase_order_id, catalog_item_id, warehouse_id, description, qty_ordered, unit_cost)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			poID, l.CatalogItemID, l.WarehouseID, nullStr(l.Description), round4(l.QtyOrdered), round4(l.UnitCost)); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return getPurchaseOrder(ctx, sch, pool, poID)
}

//encore:api auth method=GET path=/api/v1/inventory/purchase-orders
func ListPurchaseOrders(ctx context.Context, p *ListPurchaseOrdersParams) (*ListPurchaseOrdersResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	page, pageSize := p.Page, p.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	where := "WHERE deleted_at IS NULL"
	args := []any{}
	idx := 1
	if s := strings.TrimSpace(p.Status); s != "" {
		where += fmt.Sprintf(" AND status = $%d", idx)
		args = append(args, s)
		idx++
	}
	if q := strings.TrimSpace(p.Q); q != "" {
		where += fmt.Sprintf(" AND (po_no ILIKE $%d OR supplier_name ILIKE $%d)", idx, idx)
		args = append(args, "%"+q+"%")
		idx++
	}

	var total int
	if err := qrow(ctx, sch, pool,
		fmt.Sprintf(`SELECT COUNT(*) FROM pur_purchase_order %s`, where), args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := qquery(ctx, sch, pool, fmt.Sprintf(`
		SELECT id, po_no, COALESCE(supplier_name,''), contact_id::text, warehouse_id::text, status,
		       transaction_date::text, COALESCE(note,''), subtotal, created_at
		FROM pur_purchase_order
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	out := make([]PurchaseOrder, 0)
	for rows.Next() {
		po, serr := scanPurchaseOrderHeader(rows.Scan)
		if serr != nil {
			return nil, appErrs.Internal(serr.Error())
		}
		po.Lines = []PurchaseOrderLine{}
		out = append(out, po)
	}
	return &ListPurchaseOrdersResponse{PurchaseOrders: out, Total: total, Page: page, PageSize: pageSize}, nil
}

//encore:api auth method=GET path=/api/v1/inventory/purchase-orders/:id
func GetPurchaseOrder(ctx context.Context, id string) (*PurchaseOrder, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()
	return getPurchaseOrder(ctx, sch, pool, id)
}

//encore:api auth method=POST path=/api/v1/inventory/purchase-orders/:id/close
func ClosePurchaseOrder(ctx context.Context, id string) (*PurchaseOrder, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	res, err := qexec(ctx, sch, pool, `
		UPDATE pur_purchase_order SET status = 'closed', updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL AND status IN ('open','partial')`, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, appErrs.BadRequest("PO tidak bisa ditutup (tidak ditemukan atau status tidak open/partial)")
	}
	return getPurchaseOrder(ctx, sch, pool, id)
}

//encore:api auth method=POST path=/api/v1/inventory/purchase-orders/:id/cancel
func CancelPurchaseOrder(ctx context.Context, id string) (*PurchaseOrder, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	var received float64
	if err := qrow(ctx, sch, pool, `
		SELECT COALESCE(SUM(qty_received),0) FROM pur_purchase_order_line WHERE purchase_order_id = $1`,
		id).Scan(&received); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if received > epsilon {
		return nil, appErrs.BadRequest("PO sudah ada penerimaan, tidak bisa dibatalkan")
	}
	res, err := qexec(ctx, sch, pool, `
		UPDATE pur_purchase_order SET status = 'cancelled', updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL AND status NOT IN ('cancelled','received','closed')`, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, appErrs.BadRequest("PO tidak bisa dibatalkan pada status saat ini")
	}
	return getPurchaseOrder(ctx, sch, pool, id)
}

// ---------- helpers ----------

func getPurchaseOrder(ctx context.Context, sch appdb.SchemaSQL, q querier, id string) (*PurchaseOrder, error) {
	po, err := scanPurchaseOrderHeader(qrow(ctx, sch, q, `
		SELECT id, po_no, COALESCE(supplier_name,''), contact_id::text, warehouse_id::text, status,
		       transaction_date::text, COALESCE(note,''), subtotal, created_at
		FROM pur_purchase_order
		WHERE id = $1 AND deleted_at IS NULL`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appErrs.NotFound("purchase order tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	rows, err := qquery(ctx, sch, q, `
		SELECT l.id, l.catalog_item_id, COALESCE(ci.name,''), l.warehouse_id, COALESCE(l.description,''),
		       l.qty_ordered, l.qty_received, l.unit_cost
		FROM pur_purchase_order_line l
		LEFT JOIN business_catalog_item ci ON ci.id = l.catalog_item_id
		WHERE l.purchase_order_id = $1
		ORDER BY l.created_at`, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	po.Lines = make([]PurchaseOrderLine, 0)
	for rows.Next() {
		var l PurchaseOrderLine
		if err := rows.Scan(&l.ID, &l.CatalogItemID, &l.ItemName, &l.WarehouseID, &l.Description,
			&l.QtyOrdered, &l.QtyReceived, &l.UnitCost); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		po.Lines = append(po.Lines, l)
	}
	return &po, nil
}

func scanPurchaseOrderHeader(scan func(dest ...any) error) (PurchaseOrder, error) {
	var po PurchaseOrder
	var contactID, warehouseID sql.NullString
	if err := scan(&po.ID, &po.PONo, &po.SupplierName, &contactID, &warehouseID, &po.Status,
		&po.TransactionDate, &po.Note, &po.Subtotal, &po.CreatedAt); err != nil {
		return po, err
	}
	if contactID.Valid && contactID.String != "" {
		po.ContactID = &contactID.String
	}
	if warehouseID.Valid && warehouseID.String != "" {
		po.WarehouseID = &warehouseID.String
	}
	return po, nil
}
