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

// querier is satisfied by both *sql.Conn and *sql.Tx.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

const finCatSelisihPersediaan = "Selisih Persediaan"
const finCatPenyesuaianNilai = "Penyesuaian Nilai Persediaan"

// ---------- pure helpers (unit-tested) ----------

// adjustmentPlan maps a signed adjustment quantity to a movement type/direction.
func adjustmentPlan(signedQty float64) (movementType, direction string, qty float64, ok bool) {
	if signedQty > epsilon {
		return MovementAdjustPlus, dirIn, round4(signedQty), true
	}
	if signedQty < -epsilon {
		return MovementAdjustMinus, dirOut, round4(-signedQty), true
	}
	return "", "", 0, false
}

// revaluationDelta computes the new total value and the value change for a
// revaluation that sets a new unit cost while keeping on-hand quantity constant.
func revaluationDelta(onHand, oldTotalValue, newUnitCost float64) (newTotal, delta float64) {
	newTotal = round4(onHand * newUnitCost)
	delta = round4(newTotal - oldTotalValue)
	return newTotal, delta
}

// ---------- shared DB helpers ----------

func validateWarehouse(ctx context.Context, q querier, id string) error {
	if strings.TrimSpace(id) == "" {
		return appErrs.BadRequest("gudang wajib dipilih")
	}
	var exists bool
	if err := q.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM inv_warehouse WHERE id = $1 AND deleted_at IS NULL)`, id).Scan(&exists); err != nil {
		return appErrs.Internal(err.Error())
	}
	if !exists {
		return appErrs.BadRequest("gudang tidak ditemukan")
	}
	return nil
}

func validateCatalogItem(ctx context.Context, q querier, id string) error {
	if strings.TrimSpace(id) == "" {
		return appErrs.BadRequest("item katalog wajib dipilih")
	}
	var exists bool
	if err := q.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM business_catalog_item WHERE id = $1 AND deleted_at IS NULL)`, id).Scan(&exists); err != nil {
		return appErrs.Internal(err.Error())
	}
	if !exists {
		return appErrs.BadRequest("item katalog tidak ditemukan")
	}
	return nil
}

// ensureSku creates the inv_sku config row for an item if it does not exist yet,
// marking it stock-tracked. This is how an item starts being tracked (via opening
// balance / adjustment).
func ensureSku(ctx context.Context, q querier, catalogItemID string) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO inv_sku (catalog_item_id, track_stock)
		VALUES ($1, true)
		ON CONFLICT (catalog_item_id) DO NOTHING`, catalogItemID)
	return err
}

type costingContext struct {
	method        string
	blockNegative bool
}

func loadCostingContext(ctx context.Context, q querier, catalogItemID string) (costingContext, error) {
	cc := costingContext{method: CostingAverage, blockNegative: true}
	var defMethod string
	var block bool
	err := q.QueryRowContext(ctx,
		`SELECT default_costing_method, block_negative_stock FROM inv_setting ORDER BY created_at LIMIT 1`).
		Scan(&defMethod, &block)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return cc, appErrs.Internal(err.Error())
	}
	if err == nil {
		cc.blockNegative = block
	}
	var override sql.NullString
	oerr := q.QueryRowContext(ctx,
		`SELECT costing_method FROM inv_sku WHERE catalog_item_id = $1`, catalogItemID).Scan(&override)
	if oerr != nil && !errors.Is(oerr, sql.ErrNoRows) {
		return cc, appErrs.Internal(oerr.Error())
	}
	cc.method = effectiveCostingMethod(override.String, defMethod)
	return cc, nil
}

func parseDatePtr(s *string) (*time.Time, error) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", strings.TrimSpace(*s))
	if err != nil {
		return nil, appErrs.BadRequest("format tanggal harus YYYY-MM-DD")
	}
	return &t, nil
}

// ---------- response ----------

type StockOpResult struct {
	TransactionID string   `json:"transactionId,omitempty"`
	DocNo         string   `json:"docNo,omitempty"`
	MovementIDs   []string `json:"movementIds"`
	QtyAfter      float64  `json:"qtyAfter"`
	TotalCost     float64  `json:"totalCost"`
	Shortfall     float64  `json:"shortfall,omitempty"`
}

// ---------- adjustment ----------

type AdjustmentParams struct {
	CatalogItemID string  `json:"catalogItemId"`
	WarehouseID   string  `json:"warehouseId"`
	Qty           float64 `json:"qty"` // signed: positive = tambah, negative = kurangi
	UnitCost      float64 `json:"unitCost"`
	BatchNo       string  `json:"batchNo"`
	ExpiryDate    *string `json:"expiryDate"`
	Note          string  `json:"note"`
}

//encore:api auth method=POST path=/api/v1/inventory/adjustments
func CreateAdjustment(ctx context.Context, p *AdjustmentParams) (*StockOpResult, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	mtype, dir, qty, ok := adjustmentPlan(p.Qty)
	if !ok {
		return nil, appErrs.BadRequest("qty penyesuaian tidak boleh 0")
	}
	if dir == dirIn && p.UnitCost < 0 {
		return nil, appErrs.BadRequest("harga pokok tidak boleh negatif")
	}
	expiry, err := parseDatePtr(p.ExpiryDate)
	if err != nil {
		return nil, err
	}
	// Stock removal posts an expense; fail fast if the finance period is locked.
	if dir == dirOut {
		if err := finance.CheckCurrentPeriodUnlocked(ctx, u.TenantSchema); err != nil {
			return nil, err
		}
	}

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := ensureInventoryModuleReady(ctx, conn); err != nil {
		return nil, err
	}

	if err := validateCatalogItem(ctx, conn, p.CatalogItemID); err != nil {
		return nil, err
	}
	if err := validateWarehouse(ctx, conn, p.WarehouseID); err != nil {
		return nil, err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	if err := ensureSku(ctx, tx, p.CatalogItemID); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	txnID, docNo, err := insertStockTransactionHeader(ctx, tx, TxnKindAdjustment, "", p.Note, u.AccountID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_stock_transaction
		SET catalog_item_id = $2, warehouse_id = $3, signed_qty = $4, unit_cost = $5, note = $6, updated_at = now()
		WHERE id = $1`,
		txnID, p.CatalogItemID, p.WarehouseID, p.Qty, nullFloat(p.UnitCost), nullStr(p.Note)); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	cc, err := loadCostingContext(ctx, tx, p.CatalogItemID)
	if err != nil {
		return nil, err
	}
	res, err := PostMovement(ctx, tx, MovementInput{
		CatalogItemID: p.CatalogItemID,
		WarehouseID:   p.WarehouseID,
		Type:          mtype,
		Direction:     dir,
		Qty:           qty,
		UnitCost:      p.UnitCost,
		CostingMethod: cc.method,
		BlockNegative: cc.blockNegative,
		BatchNo:       p.BatchNo,
		ExpiryDate:    expiry,
		RefType:       TxnKindAdjustment,
		RefID:         txnID,
		Note:          p.Note,
		CreatedBy:     u.AccountID,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	if dir == dirOut && res.TotalCost > 0 {
		desc := fmt.Sprintf("Penyesuaian stok keluar (%s)", strings.TrimSpace(p.Note))
		if err := finance.RecordInventoryEntry(ctx, u.TenantSchema, u.AccountID,
			res.MovementID, "expense", finCatSelisihPersediaan, desc, round2(res.TotalCost), ""); err != nil {
			return nil, err
		}
	}
	return &StockOpResult{
		TransactionID: txnID, DocNo: docNo,
		MovementIDs: []string{res.MovementID}, QtyAfter: res.QtyAfter, TotalCost: res.TotalCost, Shortfall: res.Shortfall,
	}, nil
}

// ---------- transfer ----------

type TransferParams struct {
	CatalogItemID   string  `json:"catalogItemId"`
	FromWarehouseID string  `json:"fromWarehouseId"`
	ToWarehouseID   string  `json:"toWarehouseId"`
	Qty             float64 `json:"qty"`
	Note            string  `json:"note"`
}

//encore:api auth method=POST path=/api/v1/inventory/transfers
func CreateTransfer(ctx context.Context, p *TransferParams) (*StockOpResult, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if p.Qty <= epsilon {
		return nil, appErrs.BadRequest("qty transfer harus lebih dari 0")
	}
	if strings.TrimSpace(p.FromWarehouseID) == strings.TrimSpace(p.ToWarehouseID) {
		return nil, appErrs.BadRequest("gudang asal dan tujuan tidak boleh sama")
	}

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := ensureInventoryModuleReady(ctx, conn); err != nil {
		return nil, err
	}

	if err := validateCatalogItem(ctx, conn, p.CatalogItemID); err != nil {
		return nil, err
	}
	if err := validateWarehouse(ctx, conn, p.FromWarehouseID); err != nil {
		return nil, err
	}
	if err := validateWarehouse(ctx, conn, p.ToWarehouseID); err != nil {
		return nil, err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	if err := ensureSku(ctx, tx, p.CatalogItemID); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	txnID, docNo, err := insertStockTransactionHeader(ctx, tx, TxnKindTransfer, "", p.Note, u.AccountID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_stock_transaction
		SET catalog_item_id = $2, from_warehouse_id = $3, to_warehouse_id = $4, signed_qty = $5, note = $6, updated_at = now()
		WHERE id = $1`,
		txnID, p.CatalogItemID, p.FromWarehouseID, p.ToWarehouseID, p.Qty, nullStr(p.Note)); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	cc, err := loadCostingContext(ctx, tx, p.CatalogItemID)
	if err != nil {
		return nil, err
	}
	out, err := PostMovement(ctx, tx, MovementInput{
		CatalogItemID: p.CatalogItemID,
		WarehouseID:   p.FromWarehouseID,
		Type:          MovementTransferOut,
		Direction:     dirOut,
		Qty:           round4(p.Qty),
		CostingMethod: cc.method,
		BlockNegative: cc.blockNegative,
		RefType:       TxnKindTransfer,
		RefID:         txnID,
		Note:          p.Note,
		CreatedBy:     u.AccountID,
	})
	if err != nil {
		return nil, err
	}
	in, err := PostMovement(ctx, tx, MovementInput{
		CatalogItemID:    p.CatalogItemID,
		WarehouseID:      p.ToWarehouseID,
		Type:             MovementTransferIn,
		Direction:        dirIn,
		Qty:              round4(p.Qty),
		UnitCost:         out.UnitCost, // cost passes through from source
		CostingMethod:    cc.method,
		BlockNegative:    cc.blockNegative,
		RefType:          TxnKindTransfer,
		RefID:            txnID,
		SourceMovementID: out.MovementID,
		Note:             p.Note,
		CreatedBy:        u.AccountID,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &StockOpResult{
		TransactionID: txnID, DocNo: docNo,
		MovementIDs: []string{out.MovementID, in.MovementID}, QtyAfter: in.QtyAfter, TotalCost: out.TotalCost,
	}, nil
}

// ---------- opening balance ----------

type OpeningEntry struct {
	CatalogItemID string  `json:"catalogItemId"`
	WarehouseID   string  `json:"warehouseId"`
	Qty           float64 `json:"qty"`
	UnitCost      float64 `json:"unitCost"`
	BatchNo       string  `json:"batchNo"`
	ExpiryDate    *string `json:"expiryDate"`
}

type OpeningBalanceParams struct {
	Entries []OpeningEntry `json:"entries"`
}

type OpeningBalanceResponse struct {
	TransactionID string   `json:"transactionId"`
	DocNo         string   `json:"docNo"`
	Applied       int      `json:"applied"`
	MovementIDs   []string `json:"movementIds"`
}

//encore:api auth method=POST path=/api/v1/inventory/opening-balance
func CreateOpeningBalance(ctx context.Context, p *OpeningBalanceParams) (*OpeningBalanceResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if len(p.Entries) == 0 {
		return nil, appErrs.BadRequest("tidak ada baris saldo awal")
	}
	if len(p.Entries) > 1000 {
		return nil, appErrs.BadRequest("maksimal 1000 baris per unggahan")
	}

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := ensureInventoryModuleReady(ctx, conn); err != nil {
		return nil, err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	txnID, docNo, err := insertStockTransactionHeader(ctx, tx, TxnKindOpeningBalance, "", "Saldo awal", u.AccountID)
	if err != nil {
		return nil, err
	}

	catalogIDs := make([]string, 0, len(p.Entries))
	warehouseIDs := make([]string, 0, len(p.Entries))
	for _, e := range p.Entries {
		catalogIDs = append(catalogIDs, e.CatalogItemID)
		warehouseIDs = append(warehouseIDs, e.WarehouseID)
	}
	if err := validateCatalogItemsBatch(ctx, tx, catalogIDs); err != nil {
		return nil, err
	}
	if err := validateWarehousesBatch(ctx, tx, warehouseIDs); err != nil {
		return nil, err
	}
	if err := validateOpeningBalanceEntryPairs(p.Entries); err != nil {
		return nil, err
	}
	if err := validateOpeningBalanceNotRegistered(ctx, tx, p.Entries); err != nil {
		return nil, err
	}
	ccl := newCostingContextLoader()

	ids := make([]string, 0, len(p.Entries))
	for i, e := range p.Entries {
		if e.Qty <= epsilon {
			return nil, appErrs.BadRequest(fmt.Sprintf("baris %d: qty harus lebih dari 0", i+1))
		}
		if e.UnitCost < 0 {
			return nil, appErrs.BadRequest(fmt.Sprintf("baris %d: harga pokok tidak boleh negatif", i+1))
		}
		expiry, perr := parseDatePtr(e.ExpiryDate)
		if perr != nil {
			return nil, fmt.Errorf("baris %d: %w", i+1, perr)
		}
		if err := ensureSku(ctx, tx, e.CatalogItemID); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		cc, cerr := ccl.load(ctx, tx, e.CatalogItemID)
		if cerr != nil {
			return nil, cerr
		}
		var lineID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO inv_stock_transaction_line
			  (transaction_id, catalog_item_id, warehouse_id, qty, unit_cost, batch_no, expiry_date, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			RETURNING id`,
			txnID, e.CatalogItemID, e.WarehouseID, round4(e.Qty), round4(e.UnitCost),
			nullStr(e.BatchNo), nullTime(expiry), i).Scan(&lineID); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		res, merr := PostMovement(ctx, tx, MovementInput{
			CatalogItemID: e.CatalogItemID,
			WarehouseID:   e.WarehouseID,
			Type:          MovementOpening,
			Direction:     dirIn,
			Qty:           round4(e.Qty),
			UnitCost:      e.UnitCost,
			CostingMethod: cc.method,
			BlockNegative: cc.blockNegative,
			BatchNo:       e.BatchNo,
			ExpiryDate:    expiry,
			RefType:       TxnKindOpeningBalance,
			RefID:         txnID,
			RefLineID:     lineID,
			Note:          "Saldo awal " + docNo,
			CreatedBy:     u.AccountID,
		})
		if merr != nil {
			return nil, merr
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE inv_stock_transaction_line SET movement_id = $2 WHERE id = $1`, lineID, res.MovementID); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		ids = append(ids, res.MovementID)
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &OpeningBalanceResponse{TransactionID: txnID, DocNo: docNo, Applied: len(ids), MovementIDs: ids}, nil
}

// ---------- revaluation ----------

type RevaluationParams struct {
	CatalogItemID string  `json:"catalogItemId"`
	WarehouseID   string  `json:"warehouseId"`
	NewUnitCost   float64 `json:"newUnitCost"`
	Note          string  `json:"note"`
}

//encore:api auth method=POST path=/api/v1/inventory/revaluations
func CreateRevaluation(ctx context.Context, p *RevaluationParams) (*StockOpResult, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if p.NewUnitCost < 0 {
		return nil, appErrs.BadRequest("harga pokok baru tidak boleh negatif")
	}
	if err := finance.CheckCurrentPeriodUnlocked(ctx, u.TenantSchema); err != nil {
		return nil, err
	}

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := ensureInventoryModuleReady(ctx, conn); err != nil {
		return nil, err
	}

	if err := validateCatalogItem(ctx, conn, p.CatalogItemID); err != nil {
		return nil, err
	}
	if err := validateWarehouse(ctx, conn, p.WarehouseID); err != nil {
		return nil, err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	var onHand, oldTotal float64
	err = tx.QueryRowContext(ctx, `
		SELECT on_hand, total_value FROM inv_stock_balance
		WHERE catalog_item_id = $1 AND warehouse_id = $2 FOR UPDATE`,
		p.CatalogItemID, p.WarehouseID).Scan(&onHand, &oldTotal)
	if errors.Is(err, sql.ErrNoRows) || onHand <= epsilon {
		return nil, appErrs.BadRequest("tidak ada stok untuk direvaluasi di gudang ini")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	txnID, docNo, err := insertStockTransactionHeader(ctx, tx, TxnKindRevaluation, "", p.Note, u.AccountID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_stock_transaction
		SET catalog_item_id = $2, warehouse_id = $3, new_unit_cost = $4, note = $5, updated_at = now()
		WHERE id = $1`,
		txnID, p.CatalogItemID, p.WarehouseID, p.NewUnitCost, nullStr(p.Note)); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	newTotal, delta := revaluationDelta(onHand, oldTotal, p.NewUnitCost)

	cc, err := loadCostingContext(ctx, tx, p.CatalogItemID)
	if err != nil {
		return nil, err
	}
	// Keep FIFO/LIFO layers consistent with the revalued total.
	if cc.method != CostingAverage {
		if oldTotal > epsilon {
			factor := newTotal / oldTotal
			if _, err := tx.ExecContext(ctx, `
				UPDATE inv_cost_layer SET unit_cost = ROUND(unit_cost * $3, 4)
				WHERE catalog_item_id = $1 AND warehouse_id = $2 AND qty_remaining > 0`,
				p.CatalogItemID, p.WarehouseID, factor); err != nil {
				return nil, appErrs.Internal(err.Error())
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				UPDATE inv_cost_layer SET unit_cost = $3
				WHERE catalog_item_id = $1 AND warehouse_id = $2 AND qty_remaining > 0`,
				p.CatalogItemID, p.WarehouseID, p.NewUnitCost); err != nil {
				return nil, appErrs.Internal(err.Error())
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_stock_balance SET avg_unit_cost = $3, total_value = $4, updated_at = now()
		WHERE catalog_item_id = $1 AND warehouse_id = $2`,
		p.CatalogItemID, p.WarehouseID, p.NewUnitCost, newTotal); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	dir := dirIn
	if delta < 0 {
		dir = dirOut
	}
	var movementID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO inv_stock_movement
		  (catalog_item_id, warehouse_id, movement_type, direction, qty, unit_cost, total_cost,
		   qty_after, avg_cost_after, ref_type, ref_id, note, created_by)
		VALUES ($1,$2,$3,$4,0,$5,$6,$7,$5,$8,$9,$10,$11)
		RETURNING id`,
		p.CatalogItemID, p.WarehouseID, MovementRevaluation, dir, p.NewUnitCost,
		round4(abs(delta)), onHand, TxnKindRevaluation, txnID, nullStr(p.Note), nullUUID(u.AccountID)).Scan(&movementID); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	if abs(delta) > 0 {
		flow := "income"
		if delta < 0 {
			flow = "expense"
		}
		desc := fmt.Sprintf("Revaluasi HPP (%s)", strings.TrimSpace(p.Note))
		if err := finance.RecordInventoryEntry(ctx, u.TenantSchema, u.AccountID,
			movementID, flow, finCatPenyesuaianNilai, desc, round2(abs(delta)), ""); err != nil {
			return nil, err
		}
	}
	return &StockOpResult{
		TransactionID: txnID, DocNo: docNo,
		MovementIDs: []string{movementID}, QtyAfter: onHand, TotalCost: round4(abs(delta)),
	}, nil
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
