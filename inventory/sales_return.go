package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"encore.app/wabantu/finance"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

// returnableQty is the quantity still allowed to be returned (pure).
func returnableQty(soldNet, alreadyReturned float64) float64 {
	r := round4(soldNet - alreadyReturned)
	if r < 0 {
		return 0
	}
	return r
}

type SalesReturnLineInput struct {
	CatalogItemID string  `json:"catalogItemId"`
	WarehouseID   string  `json:"warehouseId"`
	Qty           float64 `json:"qty"`
}

type CreateSalesReturnParams struct {
	OrderID string                 `json:"orderId"`
	Note    string                 `json:"note"`
	Lines   []SalesReturnLineInput `json:"lines"`
}

type SalesReturnLine struct {
	CatalogItemID string  `json:"catalogItemId"`
	ItemName      string  `json:"itemName,omitempty"`
	WarehouseID   string  `json:"warehouseId"`
	Qty           float64 `json:"qty"`
	UnitCost      float64 `json:"unitCost"`
}

type SalesReturn struct {
	ID              string            `json:"id"`
	ReturnNo        string            `json:"returnNo"`
	OrderID         *string           `json:"orderId,omitempty"`
	Status          string            `json:"status"`
	TransactionDate string            `json:"transactionDate"`
	Note            string            `json:"note,omitempty"`
	TotalCost       float64           `json:"totalCost"`
	Lines           []SalesReturnLine `json:"lines"`
	CreatedAt       time.Time         `json:"createdAt"`
}

type ListSalesReturnsParams struct {
	Q        string `query:"q"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListSalesReturnsResponse struct {
	SalesReturns []SalesReturn `json:"salesReturns"`
	Total        int           `json:"total"`
	Page         int           `json:"page"`
	PageSize     int           `json:"pageSize"`
}

//encore:api auth method=POST path=/api/v1/inventory/sales-returns
func CreateSalesReturn(ctx context.Context, p *CreateSalesReturnParams) (*SalesReturn, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.OrderID) == "" {
		return nil, appErrs.BadRequest("orderId wajib diisi")
	}
	if len(p.Lines) == 0 {
		return nil, appErrs.BadRequest("minimal 1 baris retur")
	}
	if err := finance.CheckCurrentPeriodUnlocked(ctx, u.TenantSchema); err != nil {
		return nil, err
	}

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	return createSalesReturnConn(ctx, conn, u.TenantSchema, u.AccountID, p)
}

//encore:api auth method=GET path=/api/v1/inventory/sales-returns
func ListSalesReturns(ctx context.Context, p *ListSalesReturnsParams) (*ListSalesReturnsResponse, error) {
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
		where += fmt.Sprintf(" AND return_no ILIKE $%d", idx)
		args = append(args, "%"+q+"%")
		idx++
	}
	var total int
	if err := conn.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM inv_sales_return %s`, where), args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, return_no, order_id::text, status, transaction_date::text, COALESCE(note,''), total_cost, created_at
		FROM inv_sales_return %s
		ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, idx, idx+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	out := make([]SalesReturn, 0)
	for rows.Next() {
		r, serr := scanSalesReturnHeader(rows.Scan)
		if serr != nil {
			return nil, appErrs.Internal(serr.Error())
		}
		r.Lines = []SalesReturnLine{}
		out = append(out, r)
	}
	return &ListSalesReturnsResponse{SalesReturns: out, Total: total, Page: page, PageSize: pageSize}, nil
}

//encore:api auth method=GET path=/api/v1/inventory/sales-returns/:id
func GetSalesReturn(ctx context.Context, id string) (*SalesReturn, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	return getSalesReturn(ctx, conn, id)
}

// ---------- helpers ----------

func orderReturnedQty(ctx context.Context, conn *sql.Conn, orderID string) (map[string]float64, error) {
	out := map[string]float64{}
	rows, err := conn.QueryContext(ctx, `
		SELECT srl.catalog_item_id::text, COALESCE(SUM(srl.qty),0)
		FROM inv_sales_return sr
		JOIN inv_sales_return_line srl ON srl.sales_return_id = sr.id
		WHERE sr.order_id = $1::uuid AND sr.status = 'posted' AND sr.deleted_at IS NULL
		GROUP BY srl.catalog_item_id`, orderID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	for rows.Next() {
		var item string
		var qty float64
		if err := rows.Scan(&item, &qty); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		out[item] = qty
	}
	return out, rows.Err()
}

func orderSaleMovementID(ctx context.Context, conn *sql.Conn, orderID, catalogItemID string) string {
	var id sql.NullString
	_ = conn.QueryRowContext(ctx, `
		SELECT id::text FROM inv_stock_movement
		WHERE ref_type='order' AND ref_id=$1::uuid AND catalog_item_id=$2 AND movement_type='sale_issue'
		ORDER BY created_at DESC LIMIT 1`, orderID, catalogItemID).Scan(&id)
	if id.Valid {
		return id.String
	}
	return ""
}

func getSalesReturn(ctx context.Context, conn *sql.Conn, id string) (*SalesReturn, error) {
	r, err := scanSalesReturnHeader(conn.QueryRowContext(ctx, `
		SELECT id, return_no, order_id::text, status, transaction_date::text, COALESCE(note,''), total_cost, created_at
		FROM inv_sales_return WHERE id=$1 AND deleted_at IS NULL`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appErrs.NotFound("retur tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	rows, err := conn.QueryContext(ctx, `
		SELECT srl.catalog_item_id::text, COALESCE(ci.name,''), srl.warehouse_id::text, srl.qty, srl.unit_cost
		FROM inv_sales_return_line srl
		LEFT JOIN business_catalog_item ci ON ci.id = srl.catalog_item_id
		WHERE srl.sales_return_id=$1 ORDER BY srl.created_at`, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	r.Lines = make([]SalesReturnLine, 0)
	for rows.Next() {
		var l SalesReturnLine
		if err := rows.Scan(&l.CatalogItemID, &l.ItemName, &l.WarehouseID, &l.Qty, &l.UnitCost); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		r.Lines = append(r.Lines, l)
	}
	return &r, nil
}

func scanSalesReturnHeader(scan func(dest ...any) error) (SalesReturn, error) {
	var r SalesReturn
	var orderID sql.NullString
	if err := scan(&r.ID, &r.ReturnNo, &orderID, &r.Status, &r.TransactionDate,
		&r.Note, &r.TotalCost, &r.CreatedAt); err != nil {
		return r, err
	}
	if orderID.Valid && orderID.String != "" {
		r.OrderID = &orderID.String
	}
	return r, nil
}
