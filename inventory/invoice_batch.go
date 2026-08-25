package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

const maxBatchOrderActions = 100

func isInvoiceEligibleStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "shipped" || s == "completed"
}

type EligibleInvoiceOrder struct {
	ID                 string  `json:"id"`
	Status             string  `json:"status"`
	Subtotal           float64 `json:"subtotal"`
	ContactDisplayName string  `json:"contactDisplayName,omitempty"`
	ContactPhone       string  `json:"contactPhone,omitempty"`
}

type ListEligibleInvoiceOrdersParams struct {
	Q        string `query:"q"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListEligibleInvoiceOrdersResponse struct {
	Orders   []EligibleInvoiceOrder `json:"orders"`
	Total    int                    `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"pageSize"`
}

//encore:api auth method=GET path=/api/v1/inventory/invoice-eligible/orders
func ListEligibleInvoiceOrders(ctx context.Context, p *ListEligibleInvoiceOrdersParams) (*ListEligibleInvoiceOrdersResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	page, pageSize := p.Page, p.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 25
	}

	where := `
		WHERE o.deleted_at IS NULL
		  AND LOWER(TRIM(o.status)) IN ('shipped','completed')
		  AND NOT EXISTS (
		    SELECT 1 FROM inv_invoice i
		    WHERE i.order_id = o.id AND i.deleted_at IS NULL
		  )`
	args := []any{}
	idx := 1
	if q := strings.TrimSpace(p.Q); q != "" {
		where += fmt.Sprintf(` AND (
			o.id::text ILIKE $%d
			OR COALESCE(c.display_name,'') ILIKE $%d
			OR COALESCE(c.phone_number,'') ILIKE $%d
		)`, idx, idx, idx)
		args = append(args, "%"+q+"%")
		idx++
	}

	var total int
	countSQL := `SELECT COUNT(*) FROM "order" o LEFT JOIN contact c ON c.id = o.contact_id` + where
	if err := conn.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT o.id::text, o.status, o.subtotal,
		       COALESCE(c.display_name,''), COALESCE(c.phone_number,'')
		FROM "order" o
		LEFT JOIN contact c ON c.id = o.contact_id
		%s
		ORDER BY o.created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	out := make([]EligibleInvoiceOrder, 0)
	for rows.Next() {
		var row EligibleInvoiceOrder
		if err := rows.Scan(&row.ID, &row.Status, &row.Subtotal, &row.ContactDisplayName, &row.ContactPhone); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &ListEligibleInvoiceOrdersResponse{Orders: out, Total: total, Page: page, PageSize: pageSize}, nil
}

type BatchCreateInvoicesParams struct {
	OrderIDs []string `json:"orderIds"`
}

type BatchInvoiceResultLine struct {
	OrderID   string  `json:"orderId"`
	InvoiceID string  `json:"invoiceId,omitempty"`
	InvoiceNo string  `json:"invoiceNo,omitempty"`
	Error     string  `json:"error,omitempty"`
}

type BatchCreateInvoicesResponse struct {
	Processed int                      `json:"processed"`
	Failed    int                      `json:"failed"`
	Results   []BatchInvoiceResultLine `json:"results"`
}

//encore:api auth method=POST path=/api/v1/inventory/invoice-batch
func BatchCreateInvoicesFromOrders(ctx context.Context, p *BatchCreateInvoicesParams) (*BatchCreateInvoicesResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if len(p.OrderIDs) == 0 {
		return nil, appErrs.BadRequest("minimal 1 pesanan")
	}
	if len(p.OrderIDs) > maxBatchOrderActions {
		return nil, appErrs.BadRequest(fmt.Sprintf("maksimal %d pesanan per aksi", maxBatchOrderActions))
	}

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	resp := &BatchCreateInvoicesResponse{
		Results: make([]BatchInvoiceResultLine, 0, len(p.OrderIDs)),
	}
	seen := map[string]bool{}
	for _, rawID := range p.OrderIDs {
		orderID := strings.TrimSpace(rawID)
		line := BatchInvoiceResultLine{OrderID: orderID}
		if orderID == "" {
			line.Error = "id pesanan kosong"
			resp.Failed++
			resp.Results = append(resp.Results, line)
			continue
		}
		if seen[orderID] {
			line.Error = "pesanan duplikat dalam request"
			resp.Failed++
			resp.Results = append(resp.Results, line)
			continue
		}
		seen[orderID] = true

		inv, cerr := createInvoiceFromOrderConn(ctx, conn, u.AccountID, orderID, true)
		if cerr != nil {
			line.Error = cerr.Error()
			resp.Failed++
		} else {
			line.InvoiceID = inv.ID
			line.InvoiceNo = inv.InvoiceNo
			resp.Processed++
		}
		resp.Results = append(resp.Results, line)
	}
	return resp, nil
}

// createInvoiceFromOrderConn creates an invoice for one order.
// When rejectExisting is true, returns error if invoice already exists (batch mode).
// When false, returns existing invoice (idempotent single-create API).
func createInvoiceFromOrderConn(ctx context.Context, conn *sql.Conn, accountID, orderID string, rejectExisting bool) (*Invoice, error) {
	var existingID string
	err := conn.QueryRowContext(ctx,
		`SELECT id::text FROM inv_invoice WHERE order_id=$1::uuid AND deleted_at IS NULL LIMIT 1`, orderID).Scan(&existingID)
	if err == nil {
		if rejectExisting {
			return nil, appErrs.BadRequest("pesanan sudah memiliki faktur")
		}
		return getInvoice(ctx, conn, existingID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, appErrs.Internal(err.Error())
	}

	contactID, lines, subtotal, status, err := loadOrderForInvoice(ctx, conn, orderID)
	if err != nil {
		return nil, err
	}
	if !isInvoiceEligibleStatus(status) {
		return nil, appErrs.BadRequest("status pesanan harus Dalam pengiriman atau Selesai")
	}
	if len(lines) == 0 {
		return nil, appErrs.BadRequest("pesanan tidak memiliki item")
	}

	costByItem, err := orderItemSaleCost(ctx, conn, orderID)
	if err != nil {
		return nil, err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	invoiceNo, err := nextDocNumber(ctx, tx, DocInvoice, DocInvoice)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	var invoiceID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO inv_invoice (invoice_no, order_id, contact_id, status, subtotal, total_cogs, created_by)
		VALUES ($1,$2,$3,'issued',$4,0,$5)
		RETURNING id`,
		invoiceNo, orderID, nullUUID(contactID), round4(subtotal), nullUUID(accountID)).Scan(&invoiceID); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	var totalCogs float64
	for _, l := range lines {
		cogs := round4(weightedItemCost(costByItem, l.CatalogItemID) * l.Qty)
		totalCogs += cogs
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inv_invoice_line
			  (invoice_id, catalog_item_id, order_line_id, description, qty, unit_price, cogs, warehouse_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			invoiceID, nullUUID(l.CatalogItemID), nullUUID(l.LineID), nullStr(l.Name),
			round4(l.Qty), round4(l.UnitPrice), cogs, nullUUID(l.WarehouseID)); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE inv_invoice SET total_cogs=$2, updated_at=now() WHERE id=$1`, invoiceID, round4(totalCogs)); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return getInvoice(ctx, conn, invoiceID)
}
