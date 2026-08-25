package inventory

import (
	appdb "encore.app/wabantu/shared/db"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"encore.app/wabantu/finance"
	appErrs "encore.app/wabantu/shared/errs"
)

// OrderStockItem is a tenant-agnostic view of an order line, used by the order
// service to drive stock sync without the inventory package importing order.
type OrderStockItem struct {
	LineID        string
	CatalogItemID string
	WarehouseID   string
	Qty           float64
}

const cogsRefPrefix = "cogs:"
const finCatHPP = "HPP / COGS"

// committedOrderStatuses hold stock deducted (issued).
var committedOrderStatuses = map[string]bool{
	"processing": true,
	"shipped":    true,
	"completed":  true,
	"paid":       true,
	"confirmed":  true,
}

// IsCommittedOrderStatus reports whether a status means stock should be issued.
func IsCommittedOrderStatus(status string) bool {
	return committedOrderStatuses[strings.ToLower(strings.TrimSpace(status))]
}

type reqKey struct{ item, warehouse string }

// mergeRequirements aggregates already-resolved (non-bundle) lines by item+warehouse (pure).
func mergeRequirements(lines []OrderStockItem) map[reqKey]float64 {
	out := map[reqKey]float64{}
	for _, l := range lines {
		if strings.TrimSpace(l.CatalogItemID) == "" || l.Qty <= epsilon {
			continue
		}
		out[reqKey{l.CatalogItemID, strings.TrimSpace(l.WarehouseID)}] += l.Qty
	}
	return out
}

type netEntry struct{ qty, cost float64 }

// SyncOrderStock reconciles inventory to match an order's status. It is idempotent
// (safe to call repeatedly), gated on inventory setup (no-op until setup completed),
// and atomic for the stock movements. COGS is posted in accrual mode only.
//
// committed status (processing/shipped/completed/paid/confirmed) => stock issued.
// any other status (draft/cancelled) => stock restored.
func SyncOrderStock(ctx context.Context, tenantSchema, orderID, status string, items []OrderStockItem, createdBy string) error {
	if strings.TrimSpace(orderID) == "" {
		return nil
	}
	sch, err := prepareTenant(ctx, tenantSchema)
	if err != nil {
		return err
	}
	pool := tenantDB()

	setup, postExpense, _, err := loadSyncSetting(ctx, sch, pool)
	if err != nil {
		return err
	}
	if !setup {
		return nil
	}

	defaultWarehouse, err := defaultWarehouseID(ctx, sch, pool)
	if err != nil {
		return err
	}

	required := map[reqKey]float64{}
	if IsCommittedOrderStatus(status) {
		required, err = resolveOrderRequirements(ctx, sch, pool, items, defaultWarehouse)
		if err != nil {
			return err
		}
	}
	netIssued, err := orderNetIssued(ctx, sch, pool, orderID)
	if err != nil {
		return err
	}

	keys := map[reqKey]struct{}{}
	for k := range required {
		keys[k] = struct{}{}
	}
	for k := range netIssued {
		keys[k] = struct{}{}
	}

	if len(keys) > 0 {
		tx, terr := pool.BeginTx(ctx, nil)
		if terr != nil {
			return appErrs.Internal(terr.Error())
		}
		defer tx.Rollback()

		for k := range keys {
			delta := round4(required[k] - netIssued[k].qty)
			if delta > epsilon {
				cc, cerr := loadCostingContext(ctx, sch, tx, k.item)
				if cerr != nil {
					return cerr
				}
				if _, merr := PostMovement(ctx, sch, tx, MovementInput{
					CatalogItemID: k.item, WarehouseID: k.warehouse,
					Type: MovementSaleIssue, Direction: dirOut, Qty: delta,
					CostingMethod: cc.method, BlockNegative: cc.blockNegative,
					RefType: "order", RefID: orderID, CreatedBy: createdBy,
					Note: "Stok keluar pesanan",
				}); merr != nil {
					return merr
				}
			} else if delta < -epsilon {
				restoreQty := round4(-delta)
				unitCost := 0.0
				if netIssued[k].qty > epsilon {
					unitCost = round4(netIssued[k].cost / netIssued[k].qty)
				}
				cc, cerr := loadCostingContext(ctx, sch, tx, k.item)
				if cerr != nil {
					return cerr
				}
				if _, merr := PostMovement(ctx, sch, tx, MovementInput{
					CatalogItemID: k.item, WarehouseID: k.warehouse,
					Type: MovementSaleCancelRestore, Direction: dirIn, Qty: restoreQty,
					UnitCost: unitCost, CostingMethod: cc.method, BlockNegative: false,
					RefType: "order", RefID: orderID, CreatedBy: createdBy,
					Note: "Stok kembali (pesanan batal)",
				}); merr != nil {
					return merr
				}
			}
		}
		if err := tx.Commit(); err != nil {
			return appErrs.Internal(err.Error())
		}
	}

	return resyncOrderCOGS(ctx, tenantSchema, sch, pool, orderID, postExpense, createdBy)
}

// StockShortageLine describes one item+warehouse line that lacks stock for an order.
type StockShortageLine struct {
	CatalogItemID string  `json:"catalogItemId"`
	ItemName      string  `json:"itemName"`
	WarehouseID   string  `json:"warehouseId"`
	WarehouseName string  `json:"warehouseName"`
	QtyRequired   float64 `json:"qtyRequired"`
	QtyAvailable  float64 `json:"qtyAvailable"`
	QtyShort      float64 `json:"qtyShort"`
}

// PrecheckOrderStock fails fast (before the order row is committed) when entering a
// committed status would oversell a tracked item under block_negative_stock. orderID
// may be empty for a brand-new order.
func PrecheckOrderStock(ctx context.Context, tenantSchema, orderID string, items []OrderStockItem) error {
	sch, err := prepareTenant(ctx, tenantSchema)
	if err != nil {
		return err
	}
	pool := tenantDB()

	shortages, err := analyzeOrderStockShortageConn(ctx, sch, pool, orderID, items)
	if err != nil {
		return err
	}
	if len(shortages) > 0 {
		s := shortages[0]
		return appErrs.BadRequest(fmt.Sprintf("stok tidak cukup untuk %s (tersedia %g, dibutuhkan %g)",
			s.ItemName, s.QtyAvailable, s.QtyRequired))
	}
	return nil
}

// analyzeOrderStockShortageConn returns shortage lines when block_negative_stock is on
// and committed stock would oversell. Empty slice means no shortage (or checks skipped).
func analyzeOrderStockShortageConn(ctx context.Context, sch appdb.SchemaSQL, q querier, orderID string, items []OrderStockItem) ([]StockShortageLine, error) {
	setup, _, block, err := loadSyncSetting(ctx, sch, q)
	if err != nil {
		return nil, err
	}
	if !setup || !block {
		return nil, nil
	}
	defaultWarehouse, err := defaultWarehouseID(ctx, sch, q)
	if err != nil {
		return nil, err
	}
	required, err := resolveOrderRequirements(ctx, sch, q, items, defaultWarehouse)
	if err != nil {
		return nil, err
	}
	netIssued, err := orderNetIssued(ctx, sch, q, orderID)
	if err != nil {
		return nil, err
	}

	needOnHand := make([]reqKey, 0)
	for k, req := range required {
		delta := round4(req - netIssued[k].qty)
		if delta > epsilon {
			needOnHand = append(needOnHand, k)
		}
	}
	batch := &singleOrderBatch{
		sch:              sch,
		q:                q,
		defaultWarehouse: defaultWarehouse,
		onHand:           map[reqKey]float64{},
		itemNames:        map[string]string{},
		warehouseNames:   map[string]string{},
	}
	onHandByKey, err := batch.onHandFor(ctx, needOnHand)
	if err != nil {
		return nil, err
	}

	var shortages []StockShortageLine
	for k, req := range required {
		delta := round4(req - netIssued[k].qty)
		if delta <= epsilon {
			continue
		}
		onHand := onHandByKey[k]
		if delta > onHand+epsilon {
			whID := k.warehouse
			if whID == "" {
				whID = defaultWarehouse
			}
			itemName, nerr := batch.nameForItem(ctx, k.item)
			if nerr != nil {
				return nil, nerr
			}
			whName, nerr := batch.nameForWarehouse(ctx, whID)
			if nerr != nil {
				return nil, nerr
			}
			shortages = append(shortages, StockShortageLine{
				CatalogItemID: k.item,
				ItemName:      itemName,
				WarehouseID:   whID,
				WarehouseName: whName,
				QtyRequired:   delta,
				QtyAvailable:  onHand,
				QtyShort:      round4(delta - onHand),
			})
		}
	}
	sortStockShortages(shortages)
	return shortages, nil
}

// ---------- helpers ----------

func loadSyncSetting(ctx context.Context, sch appdb.SchemaSQL, q querier) (setup, postExpense, block bool, err error) {
	err = qrow(ctx, sch, q,
		`SELECT setup_completed, purchase_posts_expense, block_negative_stock FROM inv_setting ORDER BY created_at LIMIT 1`).
		Scan(&setup, &postExpense, &block)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, true, nil
	}
	if err != nil {
		return false, false, false, appErrs.Internal(err.Error())
	}
	return setup, postExpense, block, nil
}

func defaultWarehouseID(ctx context.Context, sch appdb.SchemaSQL, q querier) (string, error) {
	var id sql.NullString
	err := qrow(ctx, sch, q,
		`SELECT id::text FROM inv_warehouse WHERE is_default = true AND deleted_at IS NULL LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", appErrs.Internal(err.Error())
	}
	return id.String, nil
}

func resolveOrderRequirements(ctx context.Context, sch appdb.SchemaSQL, q rowsQuerier, items []OrderStockItem, defaultWarehouse string) (map[reqKey]float64, error) {
	cache := newSkuBundleCache(sch, q)
	if err := cache.preload(ctx, collectCatalogIDsFromOrders(items)); err != nil {
		return nil, err
	}
	return resolveOrderRequirementsWithCache(cache, items, defaultWarehouse), nil
}

func orderNetIssued(ctx context.Context, sch appdb.SchemaSQL, q querier, orderID string) (map[reqKey]netEntry, error) {
	out := map[reqKey]netEntry{}
	if strings.TrimSpace(orderID) == "" {
		return out, nil
	}
	rows, err := qquery(ctx, sch, q, `
		SELECT catalog_item_id::text, warehouse_id::text, movement_type, qty, total_cost
		FROM inv_stock_movement
		WHERE ref_type='order' AND ref_id=$1::uuid
		  AND movement_type IN ('sale_issue','sale_cancel_restore')`, orderID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	for rows.Next() {
		var item, wh, mtype string
		var qty, cost float64
		if err := rows.Scan(&item, &wh, &mtype, &qty, &cost); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		k := reqKey{item, wh}
		e := out[k]
		if mtype == MovementSaleIssue {
			e.qty += qty
			e.cost += cost
		} else {
			e.qty -= qty
			e.cost -= cost
		}
		out[k] = e
	}
	return out, rows.Err()
}

func resyncOrderCOGS(ctx context.Context, tenantSchema string, sch appdb.SchemaSQL, q querier, orderID string, postExpense bool, createdBy string) error {
	ref := cogsRefPrefix + orderID
	// Cashflow mode already expensed the purchase; ensure no stale COGS lingers.
	if postExpense {
		return finance.RemoveInventoryEntry(ctx, tenantSchema, ref)
	}
	var net float64
	if err := qrow(ctx, sch, q, `
		SELECT COALESCE(SUM(CASE
		  WHEN movement_type='sale_issue' THEN total_cost
		  WHEN movement_type='sale_cancel_restore' THEN -total_cost
		  ELSE 0 END),0)
		FROM inv_stock_movement WHERE ref_type='order' AND ref_id=$1::uuid`, orderID).Scan(&net); err != nil {
		return appErrs.Internal(err.Error())
	}
	if err := finance.RemoveInventoryEntry(ctx, tenantSchema, ref); err != nil {
		return err
	}
	net = round2(net)
	if net <= 0 {
		return nil
	}
	walletID, err := orderIncomeWalletID(ctx, sch, q, orderID)
	if err != nil {
		return err
	}
	return finance.RecordInventoryEntry(ctx, tenantSchema, createdBy, ref,
		"expense", finCatHPP, orderCOGSDescription(orderID), net, walletID)
}

func orderCOGSDescription(orderID string) string {
	short := strings.TrimSpace(orderID)
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("HPP pesanan #%s — harga pokok penjualan", short)
}

func orderIncomeWalletID(ctx context.Context, sch appdb.SchemaSQL, q querier, orderID string) (string, error) {
	var wallet sql.NullString
	err := qrow(ctx, sch, q, `
		SELECT income_wallet_id::text FROM "order" WHERE id = $1::uuid`, orderID).Scan(&wallet)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", appErrs.Internal(err.Error())
	}
	if wallet.Valid {
		return wallet.String, nil
	}
	return "", nil
}

func itemName(ctx context.Context, sch appdb.SchemaSQL, q querier, catalogItemID string) string {
	var name string
	if err := qrow(ctx, sch, q,
		`SELECT COALESCE(name,'') FROM business_catalog_item WHERE id=$1`, catalogItemID).Scan(&name); err != nil {
		return "item"
	}
	if strings.TrimSpace(name) == "" {
		return "item"
	}
	return name
}

func warehouseName(ctx context.Context, sch appdb.SchemaSQL, q querier, warehouseID string) string {
	var name string
	if err := qrow(ctx, sch, q,
		`SELECT COALESCE(name,'') FROM inv_warehouse WHERE id=$1`, warehouseID).Scan(&name); err != nil {
		return "gudang"
	}
	if strings.TrimSpace(name) == "" {
		return "gudang"
	}
	return name
}

func sortStockShortages(lines []StockShortageLine) {
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].ItemName != lines[j].ItemName {
			return lines[i].ItemName < lines[j].ItemName
		}
		return lines[i].WarehouseName < lines[j].WarehouseName
	})
}
