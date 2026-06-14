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

const finCatPembelianPersediaan = "Pembelian Persediaan"

// ---------- pure helpers ----------

// sumBillLines totals qty*unitCost across bill lines.
func sumBillLines(lines []BillLineInput) float64 {
	var s float64
	for _, l := range lines {
		s += l.Qty * l.UnitCost
	}
	return round4(s)
}

// ---------- types ----------

type BillLineInput struct {
	CatalogItemID       string  `json:"catalogItemId"`
	WarehouseID         string  `json:"warehouseId"`
	PurchaseOrderLineID string  `json:"purchaseOrderLineId"`
	Description         string  `json:"description"`
	Qty                 float64 `json:"qty"`
	UnitCost            float64 `json:"unitCost"`
	BatchNo             string  `json:"batchNo"`
	ExpiryDate          *string `json:"expiryDate"`
}

type CreateBillParams struct {
	PurchaseOrderID string          `json:"purchaseOrderId"`
	SupplierName    string          `json:"supplierName"`
	ContactID       string          `json:"contactId"`
	WarehouseID     string          `json:"warehouseId"`
	TransactionDate string          `json:"transactionDate"`
	Note            string          `json:"note"`
	Lines           []BillLineInput `json:"lines"`
}

type BillLine struct {
	ID            string  `json:"id"`
	CatalogItemID string  `json:"catalogItemId"`
	ItemName      string  `json:"itemName,omitempty"`
	WarehouseID   string  `json:"warehouseId"`
	Description   string  `json:"description,omitempty"`
	Qty           float64 `json:"qty"`
	UnitCost      float64 `json:"unitCost"`
	BatchNo       *string `json:"batchNo,omitempty"`
}

type Bill struct {
	ID              string     `json:"id"`
	BillNo          string     `json:"billNo"`
	PurchaseOrderID *string    `json:"purchaseOrderId,omitempty"`
	SupplierName    string     `json:"supplierName,omitempty"`
	Status          string     `json:"status"`
	TransactionDate string     `json:"transactionDate"`
	Note            string     `json:"note,omitempty"`
	Subtotal        float64    `json:"subtotal"`
	Lines           []BillLine `json:"lines"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type ListBillsParams struct {
	Q        string `query:"q"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListBillsResponse struct {
	Bills    []Bill `json:"bills"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

// ---------- endpoints ----------

//encore:api auth method=POST path=/api/v1/inventory/bills
func CreateBill(ctx context.Context, p *CreateBillParams) (*Bill, error) {
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

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	setting, err := loadSetting(ctx, conn)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	// Cashflow mode posts an expense — fail fast if the finance period is locked.
	if setting.PurchasePostsExpense {
		if err := finance.CheckCurrentPeriodUnlocked(ctx, u.TenantSchema); err != nil {
			return nil, err
		}
	}

	defaultWarehouse := strings.TrimSpace(p.WarehouseID)
	for i := range p.Lines {
		l := &p.Lines[i]
		if l.Qty <= epsilon {
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
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	billNo, err := nextDocNumber(ctx, tx, DocBill, DocBill)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	subtotal := sumBillLines(p.Lines)

	var billID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO pur_bill
		  (bill_no, purchase_order_id, supplier_name, contact_id, warehouse_id, status,
		   transaction_date, note, subtotal, created_by)
		VALUES ($1,$2,$3,$4,$5,'posted',$6,$7,$8,$9)
		RETURNING id`,
		billNo, nullUUID(p.PurchaseOrderID), nullStr(p.SupplierName), nullUUID(p.ContactID),
		nullUUID(defaultWarehouse), txnDate, nullStr(p.Note), subtotal, nullUUID(u.AccountID)).Scan(&billID); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	for _, l := range p.Lines {
		expiry, perr := parseDatePtr(l.ExpiryDate)
		if perr != nil {
			return nil, perr
		}
		var lineID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO pur_bill_line
			  (bill_id, purchase_order_line_id, catalog_item_id, warehouse_id, description, qty, unit_cost, batch_no, expiry_date)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			RETURNING id`,
			billID, nullUUID(l.PurchaseOrderLineID), l.CatalogItemID, l.WarehouseID,
			nullStr(l.Description), round4(l.Qty), round4(l.UnitCost), nullStr(l.BatchNo), nullTime(expiry)).Scan(&lineID); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if err := ensureSku(ctx, tx, l.CatalogItemID); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		cc, cerr := loadCostingContext(ctx, tx, l.CatalogItemID)
		if cerr != nil {
			return nil, cerr
		}
		res, merr := PostMovement(ctx, tx, MovementInput{
			CatalogItemID: l.CatalogItemID,
			WarehouseID:   l.WarehouseID,
			Type:          MovementPurchaseReceive,
			Direction:     dirIn,
			Qty:           round4(l.Qty),
			UnitCost:      round4(l.UnitCost),
			CostingMethod: cc.method,
			BlockNegative: cc.blockNegative,
			BatchNo:       l.BatchNo,
			ExpiryDate:    expiry,
			RefType:       "bill",
			RefID:         billID,
			RefLineID:     lineID,
			Note:          "Penerimaan barang " + billNo,
			CreatedBy:     u.AccountID,
		})
		if merr != nil {
			return nil, merr
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE pur_bill_line SET movement_id = $2 WHERE id = $1`, lineID, res.MovementID); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if strings.TrimSpace(l.PurchaseOrderLineID) != "" {
			if _, err := tx.ExecContext(ctx,
				`UPDATE pur_purchase_order_line SET qty_received = qty_received + $2 WHERE id = $1`,
				l.PurchaseOrderLineID, round4(l.Qty)); err != nil {
				return nil, appErrs.Internal(err.Error())
			}
		}
	}

	// Recompute linked PO status from receipts.
	if strings.TrimSpace(p.PurchaseOrderID) != "" {
		var ordered, received float64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(qty_ordered),0), COALESCE(SUM(qty_received),0)
			FROM pur_purchase_order_line WHERE purchase_order_id = $1`,
			p.PurchaseOrderID).Scan(&ordered, &received); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE pur_purchase_order SET status = $2, updated_at = now()
			WHERE id = $1 AND status IN ('open','partial')`,
			p.PurchaseOrderID, poStatusFromReceipts(ordered, received)); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	// Cashflow mode: purchase recognized as expense now (no COGS later). Accrual
	// mode (default): inventory value rises, expense deferred to COGS at sale.
	if setting.PurchasePostsExpense && subtotal > 0 {
		desc := fmt.Sprintf("Pembelian persediaan %s", billNo)
		if err := finance.RecordInventoryEntry(ctx, u.TenantSchema, u.AccountID,
			billID, "expense", finCatPembelianPersediaan, desc, round2(subtotal)); err != nil {
			return nil, err
		}
	}
	return getBill(ctx, conn, billID)
}

//encore:api auth method=GET path=/api/v1/inventory/bills
func ListBills(ctx context.Context, p *ListBillsParams) (*ListBillsResponse, error) {
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
		where += fmt.Sprintf(" AND (bill_no ILIKE $%d OR supplier_name ILIKE $%d)", idx, idx)
		args = append(args, "%"+q+"%")
		idx++
	}
	var total int
	if err := conn.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM pur_bill %s`, where), args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, bill_no, purchase_order_id::text, COALESCE(supplier_name,''), status,
		       transaction_date::text, COALESCE(note,''), subtotal, created_at
		FROM pur_bill %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	out := make([]Bill, 0)
	for rows.Next() {
		b, serr := scanBillHeader(rows.Scan)
		if serr != nil {
			return nil, appErrs.Internal(serr.Error())
		}
		b.Lines = []BillLine{}
		out = append(out, b)
	}
	return &ListBillsResponse{Bills: out, Total: total, Page: page, PageSize: pageSize}, nil
}

//encore:api auth method=GET path=/api/v1/inventory/bills/:id
func GetBill(ctx context.Context, id string) (*Bill, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	return getBill(ctx, conn, id)
}

// ---------- helpers ----------

func getBill(ctx context.Context, conn *sql.Conn, id string) (*Bill, error) {
	b, err := scanBillHeader(conn.QueryRowContext(ctx, `
		SELECT id, bill_no, purchase_order_id::text, COALESCE(supplier_name,''), status,
		       transaction_date::text, COALESCE(note,''), subtotal, created_at
		FROM pur_bill WHERE id = $1 AND deleted_at IS NULL`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appErrs.NotFound("bill tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	rows, err := conn.QueryContext(ctx, `
		SELECT l.id, l.catalog_item_id, COALESCE(ci.name,''), l.warehouse_id, COALESCE(l.description,''),
		       l.qty, l.unit_cost, l.batch_no
		FROM pur_bill_line l
		LEFT JOIN business_catalog_item ci ON ci.id = l.catalog_item_id
		WHERE l.bill_id = $1
		ORDER BY l.created_at`, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	b.Lines = make([]BillLine, 0)
	for rows.Next() {
		var l BillLine
		var batch sql.NullString
		if err := rows.Scan(&l.ID, &l.CatalogItemID, &l.ItemName, &l.WarehouseID, &l.Description,
			&l.Qty, &l.UnitCost, &batch); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		l.BatchNo = nullStrPtr(batch)
		b.Lines = append(b.Lines, l)
	}
	return &b, nil
}

func scanBillHeader(scan func(dest ...any) error) (Bill, error) {
	var b Bill
	var poID sql.NullString
	if err := scan(&b.ID, &b.BillNo, &poID, &b.SupplierName, &b.Status,
		&b.TransactionDate, &b.Note, &b.Subtotal, &b.CreatedAt); err != nil {
		return b, err
	}
	if poID.Valid && poID.String != "" {
		b.PurchaseOrderID = &poID.String
	}
	return b, nil
}
