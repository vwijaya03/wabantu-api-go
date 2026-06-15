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

const (
	TxnKindAdjustment     = "adjustment"
	TxnKindTransfer       = "transfer"
	TxnKindOpeningBalance = "opening_balance"
	TxnKindRevaluation    = "revaluation"
)

type StockTransactionLine struct {
	ID            string   `json:"id"`
	CatalogItemID string   `json:"catalogItemId"`
	ItemName      string   `json:"itemName,omitempty"`
	WarehouseID   string   `json:"warehouseId"`
	WarehouseName string   `json:"warehouseName,omitempty"`
	Qty           float64  `json:"qty"`
	UnitCost      float64  `json:"unitCost"`
	BatchNo       *string  `json:"batchNo,omitempty"`
	MovementID    *string  `json:"movementId,omitempty"`
}

type StockTransaction struct {
	ID              string                 `json:"id"`
	DocNo           string                 `json:"docNo"`
	Kind            string                 `json:"kind"`
	TransactionDate string                 `json:"transactionDate"`
	Note            string                 `json:"note,omitempty"`
	CatalogItemID   string                 `json:"catalogItemId,omitempty"`
	WarehouseID     string                 `json:"warehouseId,omitempty"`
	FromWarehouseID string                 `json:"fromWarehouseId,omitempty"`
	ToWarehouseID   string                 `json:"toWarehouseId,omitempty"`
	SignedQty       *float64               `json:"signedQty,omitempty"`
	UnitCost        *float64               `json:"unitCost,omitempty"`
	NewUnitCost     *float64               `json:"newUnitCost,omitempty"`
	ItemName        string                 `json:"itemName,omitempty"`
	WarehouseName   string                 `json:"warehouseName,omitempty"`
	FromWarehouseName string               `json:"fromWarehouseName,omitempty"`
	ToWarehouseName string                 `json:"toWarehouseName,omitempty"`
	LineCount       int                    `json:"lineCount,omitempty"`
	Lines           []StockTransactionLine `json:"lines,omitempty"`
	CreatedAt       time.Time              `json:"createdAt"`
}

type ListStockTransactionsParams struct {
	Kind     string `query:"kind"`
	Q        string `query:"q"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListStockTransactionsResponse struct {
	Transactions []StockTransaction `json:"transactions"`
	Total        int                `json:"total"`
	Page         int                `json:"page"`
	PageSize     int                `json:"pageSize"`
}

//encore:api auth method=GET path=/api/v1/inventory/stock-transactions
func ListStockTransactions(ctx context.Context, p *ListStockTransactionsParams) (*ListStockTransactionsResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := ensureInventoryModuleSchema(ctx, u.TenantSchema); err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := ensureStockTxnBackfill(ctx, conn); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	page, pageSize := p.Page, p.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 25
	}
	where := "WHERE 1=1"
	args := []any{}
	idx := 1
	if k := strings.TrimSpace(p.Kind); k != "" {
		where += fmt.Sprintf(" AND t.kind = $%d", idx)
		args = append(args, k)
		idx++
	}
	if q := strings.TrimSpace(p.Q); q != "" {
		where += fmt.Sprintf(" AND t.doc_no ILIKE $%d", idx)
		args = append(args, "%"+q+"%")
		idx++
	}
	var total int
	if err := conn.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM inv_stock_transaction t %s`, where), args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT t.id, t.doc_no, t.kind, t.transaction_date::text, COALESCE(t.note,''),
		       COALESCE(t.catalog_item_id::text,''), COALESCE(t.warehouse_id::text,''),
		       COALESCE(t.from_warehouse_id::text,''), COALESCE(t.to_warehouse_id::text,''),
		       t.signed_qty, t.unit_cost, t.new_unit_cost, t.created_at,
		       COALESCE(ci.name,''), COALESCE(w.name,''), COALESCE(wf.name,''), COALESCE(wt.name,''),
		       COALESCE(lc.line_count, 0)
		FROM (
			SELECT id, doc_no, kind, transaction_date, note,
			       catalog_item_id, warehouse_id, from_warehouse_id, to_warehouse_id,
			       signed_qty, unit_cost, new_unit_cost, created_at
			FROM inv_stock_transaction t
			%s
			ORDER BY t.created_at DESC, t.id DESC
			LIMIT $%d OFFSET $%d
		) t
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS line_count
			FROM inv_stock_transaction_line l
			WHERE l.transaction_id = t.id
		) lc ON true
		LEFT JOIN business_catalog_item ci ON ci.id = t.catalog_item_id
		LEFT JOIN inv_warehouse w ON w.id = t.warehouse_id
		LEFT JOIN inv_warehouse wf ON wf.id = t.from_warehouse_id
		LEFT JOIN inv_warehouse wt ON wt.id = t.to_warehouse_id`, where, idx, idx+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	out := make([]StockTransaction, 0)
	for rows.Next() {
		txn, serr := scanStockTxnListRow(rows.Scan)
		if serr != nil {
			return nil, appErrs.Internal(serr.Error())
		}
		out = append(out, txn)
	}
	return &ListStockTransactionsResponse{Transactions: out, Total: total, Page: page, PageSize: pageSize}, nil
}

//encore:api auth method=GET path=/api/v1/inventory/stock-transactions/:id
func GetStockTransaction(ctx context.Context, id string) (*StockTransaction, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := ensureInventoryModuleSchema(ctx, u.TenantSchema); err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	return loadStockTransaction(ctx, conn, id)
}

//encore:api auth method=DELETE path=/api/v1/inventory/stock-transactions/:id
func DeleteStockTransaction(ctx context.Context, id string) error {
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
	if err := ensureInventoryModuleReady(ctx, conn); err != nil {
		return err
	}

	txn, err := loadStockTransaction(ctx, conn, id)
	if err != nil {
		return err
	}
	if err := finance.CheckPeriodUnlockedForDate(ctx, u.TenantSchema, txn.TransactionDate); err != nil {
		return err
	}

	movs, err := collectMovementsByRef(ctx, conn, txnKindRefType(txn.Kind), id)
	if err != nil {
		return err
	}
	refs := movementFinanceRefs("", movs)
	if err := finance.RemoveInventoryEntries(ctx, u.TenantSchema, refs); err != nil {
		return err
	}

	tx, terr := conn.BeginTx(ctx, nil)
	if terr != nil {
		return appErrs.Internal(terr.Error())
	}
	defer tx.Rollback()

	if _, err := purgeMovementsByRef(ctx, tx, txnKindRefType(txn.Kind), id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM inv_stock_transaction WHERE id = $1`, id); err != nil {
		return appErrs.Internal(err.Error())
	}
	return tx.Commit()
}

func txnKindRefType(kind string) string {
	return kind
}

func docTypeForKind(kind string) (docType, prefix string, err error) {
	switch kind {
	case TxnKindAdjustment:
		return DocAdjust, DocAdjust, nil
	case TxnKindTransfer:
		return DocTransfer, DocTransfer, nil
	case TxnKindOpeningBalance:
		return DocOpening, DocOpening, nil
	case TxnKindRevaluation:
		return DocRevalue, DocRevalue, nil
	default:
		return "", "", appErrs.BadRequest("jenis transaksi tidak dikenal")
	}
}

func insertStockTransactionHeader(ctx context.Context, tx *sql.Tx, kind, txnDate, note, createdBy string) (id, docNo string, err error) {
	docType, prefix, derr := docTypeForKind(kind)
	if derr != nil {
		return "", "", derr
	}
	docNo, err = nextDocNumber(ctx, tx, docType, prefix)
	if err != nil {
		return "", "", appErrs.Internal(err.Error())
	}
	if strings.TrimSpace(txnDate) == "" {
		txnDate = time.Now().Format("2006-01-02")
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO inv_stock_transaction (doc_no, kind, transaction_date, note, created_by)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id`, docNo, kind, txnDate, nullStr(note), nullUUID(createdBy)).Scan(&id)
	if err != nil {
		return "", "", appErrs.Internal(err.Error())
	}
	return id, docNo, nil
}

func loadStockTransaction(ctx context.Context, conn *sql.Conn, id string) (*StockTransaction, error) {
	row := conn.QueryRowContext(ctx, `
		SELECT id, doc_no, kind, transaction_date::text, COALESCE(note,''),
		       COALESCE(catalog_item_id::text,''), COALESCE(warehouse_id::text,''),
		       COALESCE(from_warehouse_id::text,''), COALESCE(to_warehouse_id::text,''),
		       signed_qty, unit_cost, new_unit_cost, created_at
		FROM inv_stock_transaction WHERE id = $1`, id)
	txn, err := scanStockTxnHeader(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appErrs.NotFound("transaksi tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	rows, err := conn.QueryContext(ctx, `
		SELECT l.id, l.catalog_item_id, COALESCE(ci.name,''), l.warehouse_id, COALESCE(w.name,''),
		       l.qty, l.unit_cost, l.batch_no, l.movement_id::text
		FROM inv_stock_transaction_line l
		LEFT JOIN business_catalog_item ci ON ci.id = l.catalog_item_id
		LEFT JOIN inv_warehouse w ON w.id = l.warehouse_id
		WHERE l.transaction_id = $1
		ORDER BY l.sort_order, l.created_at`, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	txn.Lines = make([]StockTransactionLine, 0)
	for rows.Next() {
		var l StockTransactionLine
		var batch, movID sql.NullString
		if err := rows.Scan(&l.ID, &l.CatalogItemID, &l.ItemName, &l.WarehouseID, &l.WarehouseName,
			&l.Qty, &l.UnitCost, &batch, &movID); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		l.BatchNo = nullStrPtr(batch)
		if movID.Valid {
			l.MovementID = &movID.String
		}
		txn.Lines = append(txn.Lines, l)
	}
	return &txn, nil
}

func scanStockTxnListRow(scan func(dest ...any) error) (StockTransaction, error) {
	var t StockTransaction
	var signed, unit, newUnit sql.NullFloat64
	if err := scan(&t.ID, &t.DocNo, &t.Kind, &t.TransactionDate, &t.Note,
		&t.CatalogItemID, &t.WarehouseID, &t.FromWarehouseID, &t.ToWarehouseID,
		&signed, &unit, &newUnit, &t.CreatedAt,
		&t.ItemName, &t.WarehouseName, &t.FromWarehouseName, &t.ToWarehouseName, &t.LineCount); err != nil {
		return t, err
	}
	if signed.Valid {
		v := signed.Float64
		t.SignedQty = &v
	}
	if unit.Valid {
		v := unit.Float64
		t.UnitCost = &v
	}
	if newUnit.Valid {
		v := newUnit.Float64
		t.NewUnitCost = &v
	}
	return t, nil
}

func scanStockTxnHeader(scan func(dest ...any) error) (StockTransaction, error) {
	var t StockTransaction
	var signed, unit, newUnit sql.NullFloat64
	if err := scan(&t.ID, &t.DocNo, &t.Kind, &t.TransactionDate, &t.Note,
		&t.CatalogItemID, &t.WarehouseID, &t.FromWarehouseID, &t.ToWarehouseID,
		&signed, &unit, &newUnit, &t.CreatedAt); err != nil {
		return t, err
	}
	if signed.Valid {
		v := signed.Float64
		t.SignedQty = &v
	}
	if unit.Valid {
		v := unit.Float64
		t.UnitCost = &v
	}
	if newUnit.Valid {
		v := newUnit.Float64
		t.NewUnitCost = &v
	}
	return t, nil
}

// resolveRefDocNo maps movement ref to human-readable document number.
func resolveRefDocNo(ctx context.Context, q querier, refType string, refID string) string {
	if refID == "" {
		return ""
	}
	var docNo string
	switch refType {
	case "bill":
		_ = q.QueryRowContext(ctx, `SELECT bill_no FROM pur_bill WHERE id = $1`, refID).Scan(&docNo)
	case "order":
		return formatOrderRef(refID)
	case TxnKindAdjustment, TxnKindTransfer, TxnKindOpeningBalance, TxnKindRevaluation:
		_ = q.QueryRowContext(ctx, `SELECT doc_no FROM inv_stock_transaction WHERE id = $1`, refID).Scan(&docNo)
	case "sales_return":
		_ = q.QueryRowContext(ctx, `SELECT return_no FROM inv_sales_return WHERE id = $1`, refID).Scan(&docNo)
	default:
		return ""
	}
	return docNo
}

func refTypeLabel(refType string) string {
	switch refType {
	case "bill":
		return "Penerimaan (Bill)"
	case "order":
		return "Pesanan"
	case TxnKindAdjustment:
		return "Penyesuaian"
	case TxnKindTransfer:
		return "Transfer"
	case TxnKindOpeningBalance:
		return "Saldo Awal"
	case TxnKindRevaluation:
		return "Revaluasi HPP"
	case "sales_return":
		return "Retur Penjualan"
	default:
		if refType == "" {
			return ""
		}
		return refType
	}
}
