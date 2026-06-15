package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

// orderLineView mirrors the order.items JSON we need (inventory cannot import order).
type orderLineView struct {
	LineID        string  `json:"lineId"`
	CatalogItemID string  `json:"catalogItemId"`
	Name          string  `json:"name"`
	Qty           float64 `json:"qty"`
	UnitPrice     float64 `json:"unitPrice"`
	WarehouseID   string  `json:"warehouseId"`
}

type InvoiceLine struct {
	CatalogItemID string  `json:"catalogItemId"`
	OrderLineID   string  `json:"orderLineId,omitempty"`
	Description   string  `json:"description"`
	Qty           float64 `json:"qty"`
	UnitPrice     float64 `json:"unitPrice"`
	Cogs          float64 `json:"cogs"`
}

type Invoice struct {
	ID              string        `json:"id"`
	InvoiceNo       string        `json:"invoiceNo"`
	OrderID         *string       `json:"orderId,omitempty"`
	Status          string        `json:"status"`
	TransactionDate string        `json:"transactionDate"`
	Subtotal        float64       `json:"subtotal"`
	TotalCogs       float64       `json:"totalCogs"`
	Note            string        `json:"note,omitempty"`
	Lines           []InvoiceLine `json:"lines"`
	CreatedAt       time.Time     `json:"createdAt"`
}

type ListInvoicesParams struct {
	Q        string `query:"q"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListInvoicesResponse struct {
	Invoices []Invoice `json:"invoices"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
}

//encore:api auth method=POST path=/api/v1/inventory/invoices/from-order/:orderID
func CreateInvoiceFromOrder(ctx context.Context, orderID string) (*Invoice, error) {
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
	return createInvoiceFromOrderConn(ctx, conn, u.AccountID, orderID, false)
}

//encore:api auth method=GET path=/api/v1/inventory/invoices
func ListInvoices(ctx context.Context, p *ListInvoicesParams) (*ListInvoicesResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

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
	if q := strings.TrimSpace(p.Q); q != "" {
		where += " AND invoice_no ILIKE $1"
		args = append(args, "%"+q+"%")
		idx++
	}
	var total int
	if err := conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM inv_invoice "+where, args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, invoice_no, order_id::text, status, transaction_date::text, subtotal, total_cogs, COALESCE(note,''), created_at
		FROM inv_invoice %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, idx, idx+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	out := make([]Invoice, 0)
	for rows.Next() {
		inv, serr := scanInvoiceHeader(rows.Scan)
		if serr != nil {
			return nil, appErrs.Internal(serr.Error())
		}
		inv.Lines = []InvoiceLine{}
		out = append(out, inv)
	}
	return &ListInvoicesResponse{Invoices: out, Total: total, Page: page, PageSize: pageSize}, nil
}

//encore:api auth method=GET path=/api/v1/inventory/invoices/:id
func GetInvoice(ctx context.Context, id string) (*Invoice, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	return getInvoice(ctx, conn, id)
}

// ---------- helpers ----------

func loadOrderForInvoice(ctx context.Context, conn *sql.Conn, orderID string) (contactID string, lines []orderLineView, subtotal float64, status string, err error) {
	var itemsRaw []byte
	var cid sql.NullString
	e := conn.QueryRowContext(ctx, `
		SELECT contact_id::text, COALESCE(items,'[]'), subtotal, status
		FROM "order" WHERE id=$1::uuid AND deleted_at IS NULL`, orderID).Scan(&cid, &itemsRaw, &subtotal, &status)
	if errors.Is(e, sql.ErrNoRows) {
		return "", nil, 0, "", appErrs.NotFound("pesanan tidak ditemukan")
	}
	if e != nil {
		return "", nil, 0, "", appErrs.Internal(e.Error())
	}
	if len(itemsRaw) > 0 {
		_ = json.Unmarshal(itemsRaw, &lines)
	}
	return cid.String, lines, subtotal, status, nil
}

func orderItemSaleCost(ctx context.Context, conn *sql.Conn, orderID string) (map[string]netEntry, error) {
	out := map[string]netEntry{}
	rows, err := conn.QueryContext(ctx, `
		SELECT catalog_item_id::text, movement_type, qty, total_cost
		FROM inv_stock_movement
		WHERE ref_type='order' AND ref_id=$1::uuid AND movement_type IN ('sale_issue','sale_cancel_restore')`, orderID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	for rows.Next() {
		var item, mtype string
		var qty, cost float64
		if err := rows.Scan(&item, &mtype, &qty, &cost); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		e := out[item]
		if mtype == MovementSaleIssue {
			e.qty += qty
			e.cost += cost
		} else {
			e.qty -= qty
			e.cost -= cost
		}
		out[item] = e
	}
	return out, rows.Err()
}

func weightedItemCost(m map[string]netEntry, item string) float64 {
	e := m[item]
	if e.qty <= epsilon {
		return 0
	}
	return round4(e.cost / e.qty)
}

func getInvoice(ctx context.Context, conn *sql.Conn, id string) (*Invoice, error) {
	inv, err := scanInvoiceHeader(conn.QueryRowContext(ctx, `
		SELECT id, invoice_no, order_id::text, status, transaction_date::text, subtotal, total_cogs, COALESCE(note,''), created_at
		FROM inv_invoice WHERE id=$1 AND deleted_at IS NULL`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appErrs.NotFound("invoice tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	rows, err := conn.QueryContext(ctx, `
		SELECT catalog_item_id::text, order_line_id::text, COALESCE(description,''), qty, unit_price, cogs
		FROM inv_invoice_line WHERE invoice_id=$1 ORDER BY created_at`, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	inv.Lines = make([]InvoiceLine, 0)
	for rows.Next() {
		var l InvoiceLine
		var item, lineID sql.NullString
		if err := rows.Scan(&item, &lineID, &l.Description, &l.Qty, &l.UnitPrice, &l.Cogs); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if item.Valid {
			l.CatalogItemID = item.String
		}
		if lineID.Valid {
			l.OrderLineID = lineID.String
		}
		inv.Lines = append(inv.Lines, l)
	}
	return &inv, nil
}

func scanInvoiceHeader(scan func(dest ...any) error) (Invoice, error) {
	var inv Invoice
	var orderID sql.NullString
	if err := scan(&inv.ID, &inv.InvoiceNo, &orderID, &inv.Status, &inv.TransactionDate,
		&inv.Subtotal, &inv.TotalCogs, &inv.Note, &inv.CreatedAt); err != nil {
		return inv, err
	}
	if orderID.Valid && orderID.String != "" {
		inv.OrderID = &orderID.String
	}
	return inv, nil
}
