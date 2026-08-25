package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

// Movement types (stored in inv_stock_movement.movement_type).
const (
	MovementOpening           = "opening_balance"
	MovementPurchaseReceive   = "purchase_receive"
	MovementSaleIssue         = "sale_issue"
	MovementSaleCancelRestore = "sale_cancel_restore"
	MovementReturnIn          = "return_in"
	MovementAdjustPlus        = "adjustment_plus"
	MovementAdjustMinus       = "adjustment_minus"
	MovementTransferOut       = "transfer_out"
	MovementTransferIn        = "transfer_in"
	MovementWriteOff          = "write_off"
	MovementRevaluation       = "revaluation_cost"
)

const (
	dirIn  = "in"
	dirOut = "out"
)

// MovementInput drives a single stock operation through the costing engine.
// UnitCost is required for "in"; for "out" the cost is computed (FIFO/LIFO/avg).
type MovementInput struct {
	CatalogItemID    string
	WarehouseID      string
	Type             string
	Direction        string
	Qty              float64
	UnitCost         float64
	CostingMethod    string
	BlockNegative    bool
	BatchNo          string
	ExpiryDate       *time.Time
	RefType          string
	RefID            string
	RefLineID        string
	SourceMovementID string
	FinanceTxnID     string
	Note             string
	CreatedBy        string
}

// MovementResult summarizes the posted movement and resulting snapshot.
type MovementResult struct {
	MovementID string
	TotalCost  float64
	UnitCost   float64
	QtyAfter   float64
	AvgAfter   float64
	Shortfall  float64
}

// PostMovement applies one stock movement atomically inside the caller's tx:
// updates cost layers (FIFO/LIFO), the balance snapshot, and appends the ledger
// row. The caller's tx connection MUST already have search_path set to the tenant
// schema (see tenant.TenantConn). Composable so Bill/Order can post several
// movements + finance entries in one transaction.
func PostMovement(ctx context.Context, tx *sql.Tx, in MovementInput) (*MovementResult, error) {
	if in.Qty <= epsilon {
		return nil, appErrs.BadRequest("qty harus lebih dari 0")
	}
	if in.Direction != dirIn && in.Direction != dirOut {
		return nil, appErrs.BadRequest("arah movement tidak valid")
	}
	method := effectiveCostingMethod(in.CostingMethod, "")

	var b BalanceState
	err := tx.QueryRowContext(ctx, `
		SELECT on_hand, avg_unit_cost, total_value
		FROM inv_stock_balance
		WHERE catalog_item_id = $1 AND warehouse_id = $2
		FOR UPDATE`, in.CatalogItemID, in.WarehouseID).
		Scan(&b.OnHand, &b.AvgCost, &b.TotalValue)
	if err == sql.ErrNoRows {
		b = BalanceState{}
	} else if err != nil {
		return nil, err
	}

	var cost, shortfall float64

	if in.Direction == dirIn {
		cost = round4(in.Qty * in.UnitCost)
		b = applyIn(b, in.Qty, in.UnitCost)
	} else {
		if method == CostingAverage {
			cost = issueCostAverage(b.AvgCost, in.Qty)
			if in.Qty > b.OnHand+epsilon {
				shortfall = round4(in.Qty - b.OnHand)
				if in.BlockNegative {
					return nil, insufficientErr(b.OnHand, in.Qty)
				}
			}
		} else {
			layers, lerr := loadLayers(ctx, tx, in.CatalogItemID, in.WarehouseID, method)
			if lerr != nil {
				return nil, lerr
			}
			draws, layerCost, sf := planConsumption(layers, in.Qty)
			shortfall = sf
			if shortfall > epsilon && in.BlockNegative {
				return nil, insufficientErr(b.OnHand, in.Qty)
			}
			cost = layerCost
			if shortfall > epsilon {
				cost = round4(cost + shortfall*b.AvgCost) // best-effort cost for uncovered qty
			}
			for _, d := range draws {
				if _, derr := tx.ExecContext(ctx,
					`UPDATE inv_cost_layer SET qty_remaining = qty_remaining - $2 WHERE id = $1`,
					d.LayerID, d.Qty); derr != nil {
					return nil, derr
				}
			}
		}
		b = applyOut(b, in.Qty, cost)
	}

	unitCost := in.UnitCost
	if in.Direction == dirOut {
		unitCost = weightedUnitCost(cost, in.Qty)
	}

	var movementID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO inv_stock_movement
		  (catalog_item_id, warehouse_id, movement_type, direction, qty, unit_cost, total_cost,
		   qty_after, avg_cost_after, batch_no, expiry_date, ref_type, ref_id, ref_line_id,
		   source_movement_id, finance_txn_id, note, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING id`,
		in.CatalogItemID, in.WarehouseID, in.Type, in.Direction, in.Qty, unitCost, cost,
		b.OnHand, b.AvgCost, nullStr(in.BatchNo), nullTime(in.ExpiryDate),
		nullStr(in.RefType), nullUUID(in.RefID), nullUUID(in.RefLineID),
		nullUUID(in.SourceMovementID), nullUUID(in.FinanceTxnID), nullStr(in.Note), nullUUID(in.CreatedBy),
	).Scan(&movementID)
	if err != nil {
		return nil, err
	}

	// FIFO/LIFO incoming stock creates a new cost layer. AVERAGE relies on the
	// snapshot average and intentionally keeps no layers.
	if in.Direction == dirIn && method != CostingAverage {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inv_cost_layer
			  (catalog_item_id, warehouse_id, qty_remaining, unit_cost, batch_no, expiry_date, source_movement_id, received_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7, now())`,
			in.CatalogItemID, in.WarehouseID, in.Qty, in.UnitCost,
			nullStr(in.BatchNo), nullTime(in.ExpiryDate), movementID); err != nil {
			return nil, err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO inv_stock_balance (catalog_item_id, warehouse_id, on_hand, avg_unit_cost, total_value, updated_at)
		VALUES ($1,$2,$3,$4,$5, now())
		ON CONFLICT (catalog_item_id, warehouse_id)
		DO UPDATE SET on_hand = $3, avg_unit_cost = $4, total_value = $5, updated_at = now()`,
		in.CatalogItemID, in.WarehouseID, b.OnHand, b.AvgCost, b.TotalValue); err != nil {
		return nil, err
	}

	return &MovementResult{
		MovementID: movementID,
		TotalCost:  cost,
		UnitCost:   unitCost,
		QtyAfter:   b.OnHand,
		AvgAfter:   b.AvgCost,
		Shortfall:  shortfall,
	}, nil
}

func loadLayers(ctx context.Context, tx *sql.Tx, itemID, warehouseID, method string) ([]Layer, error) {
	order := "received_at ASC, id ASC"
	if method == CostingLIFO {
		order = "received_at DESC, id DESC"
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, qty_remaining, unit_cost, COALESCE(batch_no, '')
		FROM inv_cost_layer
		WHERE catalog_item_id = $1 AND warehouse_id = $2 AND qty_remaining > 0
		ORDER BY %s`, order), itemID, warehouseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Layer
	for rows.Next() {
		var l Layer
		if err := rows.Scan(&l.ID, &l.QtyRemaining, &l.UnitCost, &l.BatchNo); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func insufficientErr(onHand, requested float64) error {
	return appErrs.BadRequest(fmt.Sprintf("stok tidak cukup (tersedia %g, diminta %g)", onHand, requested))
}

// ---------- read endpoints ----------

type StockRow struct {
	CatalogItemID string  `json:"catalogItemId"`
	ItemName      string  `json:"itemName"`
	ExternalCode  string  `json:"externalCode"`
	WarehouseID   string  `json:"warehouseId"`
	WarehouseName string  `json:"warehouseName"`
	OnHand        float64 `json:"onHand"`
	Reserved      float64 `json:"reserved"`
	Available     float64 `json:"available"`
	AvgUnitCost   float64 `json:"avgUnitCost"`
	TotalValue    float64 `json:"totalValue"`
}

type ListStockResponse struct {
	Stock    []StockRow `json:"stock"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
}

type ListStockParams struct {
	WarehouseID   string `query:"warehouseId"`
	CatalogItemID string `query:"catalogItemId"`
	Q             string `query:"q"`
	Page          int    `query:"page"`
	PageSize      int    `query:"pageSize"`
}

//encore:api auth method=GET path=/api/v1/inventory/stock
func ListStock(ctx context.Context, p *ListStockParams) (*ListStockResponse, error) {
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
	defer tenant.CloseTenantConn(conn)

	page, pageSize := p.Page, p.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	where := "WHERE 1=1"
	args := []any{}
	idx := 1
	if strings.TrimSpace(p.WarehouseID) != "" {
		where += fmt.Sprintf(" AND b.warehouse_id = $%d", idx)
		args = append(args, p.WarehouseID)
		idx++
	}
	if strings.TrimSpace(p.CatalogItemID) != "" {
		where += fmt.Sprintf(" AND b.catalog_item_id = $%d", idx)
		args = append(args, p.CatalogItemID)
		idx++
	}
	if q := strings.TrimSpace(p.Q); q != "" {
		where += fmt.Sprintf(" AND (ci.name ILIKE $%d OR ci.external_code ILIKE $%d)", idx, idx)
		args = append(args, "%"+q+"%")
		idx++
	}

	var total int
	if err := conn.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM inv_stock_balance b
		JOIN business_catalog_item ci ON ci.id = b.catalog_item_id
		JOIN inv_warehouse w ON w.id = b.warehouse_id
		%s`, where), args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT b.catalog_item_id, ci.name, ci.external_code, b.warehouse_id, w.name,
		       b.on_hand, b.reserved, b.avg_unit_cost, b.total_value
		FROM inv_stock_balance b
		JOIN business_catalog_item ci ON ci.id = b.catalog_item_id
		JOIN inv_warehouse w ON w.id = b.warehouse_id
		%s
		ORDER BY ci.name, w.name
		LIMIT $%d OFFSET $%d`, where, idx, idx+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	out := make([]StockRow, 0)
	for rows.Next() {
		var s StockRow
		if err := rows.Scan(&s.CatalogItemID, &s.ItemName, &s.ExternalCode, &s.WarehouseID,
			&s.WarehouseName, &s.OnHand, &s.Reserved, &s.AvgUnitCost, &s.TotalValue); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		s.Available = round4(s.OnHand - s.Reserved)
		out = append(out, s)
	}
	return &ListStockResponse{Stock: out, Total: total, Page: page, PageSize: pageSize}, nil
}

type MovementRow struct {
	ID            string    `json:"id"`
	CatalogItemID string    `json:"catalogItemId"`
	ItemName      string    `json:"itemName"`
	WarehouseID   string    `json:"warehouseId"`
	WarehouseName string    `json:"warehouseName"`
	MovementType  string    `json:"movementType"`
	Direction     string    `json:"direction"`
	Qty           float64   `json:"qty"`
	UnitCost      float64   `json:"unitCost"`
	TotalCost     float64   `json:"totalCost"`
	QtyAfter      float64   `json:"qtyAfter"`
	BatchNo       *string   `json:"batchNo,omitempty"`
	RefType       *string   `json:"refType,omitempty"`
	RefID         *string   `json:"refId,omitempty"`
	RefDocNo      *string   `json:"refDocNo,omitempty"`
	RefKind       *string   `json:"refKind,omitempty"`
	Note          *string   `json:"note,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

type ListMovementsResponse struct {
	Movements []MovementRow `json:"movements"`
	Total     int           `json:"total"`
	Page      int           `json:"page"`
	PageSize  int           `json:"pageSize"`
}

type ListMovementsParams struct {
	CatalogItemID string `query:"catalogItemId"`
	WarehouseID   string `query:"warehouseId"`
	Type          string `query:"type"`
	Q             string `query:"q"`
	Page          int    `query:"page"`
	PageSize      int    `query:"pageSize"`
}

//encore:api auth method=GET path=/api/v1/inventory/movements
func ListMovements(ctx context.Context, p *ListMovementsParams) (*ListMovementsResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)
	if err := ensureInventoryModuleReady(ctx, conn, u.TenantSchema); err != nil {
		return nil, err
	}

	if err := validateListMovementsParams(p); err != nil {
		return nil, err
	}

	page, pageSize := p.Page, p.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	where, args, idx := buildMovementListWhere(p)

	var total int
	if err := conn.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM inv_stock_movement m %s`, where), args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT m.id, m.catalog_item_id, COALESCE(ci.name, ''), m.warehouse_id, COALESCE(w.name, ''),
		       m.movement_type, m.direction, m.qty, m.unit_cost, m.total_cost, m.qty_after,
		       m.batch_no, m.ref_type, m.ref_id::text, m.note, m.created_at
		FROM inv_stock_movement m
		LEFT JOIN business_catalog_item ci ON ci.id = m.catalog_item_id
		LEFT JOIN inv_warehouse w ON w.id = m.warehouse_id
		%s
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	type movementRef struct {
		refType string
		refID   string
	}
	scanned := make([]struct {
		row MovementRow
		ref *movementRef
	}, 0)
	refKeys := make([]refDocKey, 0)
	for rows.Next() {
		var m MovementRow
		var batch, refType, refID, note sql.NullString
		if err := rows.Scan(&m.ID, &m.CatalogItemID, &m.ItemName, &m.WarehouseID, &m.WarehouseName,
			&m.MovementType, &m.Direction, &m.Qty, &m.UnitCost, &m.TotalCost, &m.QtyAfter,
			&batch, &refType, &refID, &note, &m.CreatedAt); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		m.BatchNo = nullStrPtr(batch)
		m.RefType = nullStrPtr(refType)
		m.RefID = nullStrPtr(refID)
		m.Note = nullStrPtr(note)
		entry := struct {
			row MovementRow
			ref *movementRef
		}{row: m}
		if refType.Valid && refID.Valid {
			entry.ref = &movementRef{refType: refType.String, refID: refID.String}
			refKeys = append(refKeys, refDocKey{refType: refType.String, refID: refID.String})
		}
		scanned = append(scanned, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := rows.Close(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	refDocs := batchResolveRefDocNos(ctx, conn, refKeys)
	out := make([]MovementRow, 0, len(scanned))
	for _, entry := range scanned {
		m := entry.row
		if entry.ref != nil {
			if docNo := resolveRefDocNoFromMap(refDocs, entry.ref.refType, entry.ref.refID); docNo != "" {
				m.RefDocNo = &docNo
			}
			if lbl := refTypeLabel(entry.ref.refType); lbl != "" {
				m.RefKind = &lbl
			}
		}
		out = append(out, m)
	}
	return &ListMovementsResponse{Movements: out, Total: total, Page: page, PageSize: pageSize}, nil
}

// ---------- small helpers ----------

func nullStr(s string) any {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil
	}
	return t
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func nullStrPtr(ns sql.NullString) *string {
	if !ns.Valid || strings.TrimSpace(ns.String) == "" {
		return nil
	}
	v := ns.String
	return &v
}
