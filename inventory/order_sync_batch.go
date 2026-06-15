package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

type skuMeta struct {
	exists     bool
	trackStock bool
	isBundle   bool
}

type skuBundleCache struct {
	q       rowsQuerier
	sku     map[string]skuMeta
	bundles map[string][]BundleComponent
}

func newSkuBundleCache(q rowsQuerier) *skuBundleCache {
	return &skuBundleCache{
		q:       q,
		sku:     map[string]skuMeta{},
		bundles: map[string][]BundleComponent{},
	}
}

func (c *skuBundleCache) preload(ctx context.Context, catalogIDs []string) error {
	ids := uniqueNonEmpty(catalogIDs)
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if _, ok := c.sku[id]; !ok {
			c.sku[id] = skuMeta{}
		}
	}
	clause, args := inClause(1, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := c.q.QueryContext(ctx, fmt.Sprintf(`
		SELECT catalog_item_id::text, COALESCE(track_stock, false), COALESCE(is_bundle, false)
		FROM inv_sku WHERE catalog_item_id IN (%s)`, clause), args...)
	if err != nil {
		return appErrsInternal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var track, bundle bool
		if err := rows.Scan(&id, &track, &bundle); err != nil {
			return appErrsInternal(err)
		}
		c.sku[id] = skuMeta{exists: true, trackStock: track, isBundle: bundle}
	}
	if err := rows.Err(); err != nil {
		return appErrsInternal(err)
	}

	bundleParents := make([]string, 0)
	for id, m := range c.sku {
		if m.exists && m.isBundle {
			bundleParents = append(bundleParents, id)
		}
	}
	if len(bundleParents) == 0 {
		return nil
	}
	clause, args = inClause(1, len(bundleParents))
	for i, id := range bundleParents {
		args[i] = id
	}
	brows, err := c.q.QueryContext(ctx, fmt.Sprintf(`
		SELECT parent_catalog_item_id::text, child_catalog_item_id::text, qty
		FROM inv_bundle_component WHERE parent_catalog_item_id IN (%s)`, clause), args...)
	if err != nil {
		return appErrsInternal(err)
	}
	defer brows.Close()
	for brows.Next() {
		var parent, child string
		var qty float64
		if err := brows.Scan(&parent, &child, &qty); err != nil {
			return appErrsInternal(err)
		}
		c.bundles[parent] = append(c.bundles[parent], BundleComponent{ChildCatalogItemID: child, Qty: qty})
	}
	return brows.Err()
}

func appErrsInternal(err error) error {
	if err == nil {
		return nil
	}
	return appErrs.Internal(err.Error())
}

func resolveOrderRequirementsWithCache(cache *skuBundleCache, items []OrderStockItem, defaultWarehouse string) map[reqKey]float64 {
	out := map[reqKey]float64{}
	for _, it := range items {
		if strings.TrimSpace(it.CatalogItemID) == "" || it.Qty <= epsilon {
			continue
		}
		wh := strings.TrimSpace(it.WarehouseID)
		if wh == "" {
			wh = defaultWarehouse
		}
		if wh == "" {
			continue
		}
		meta, ok := cache.sku[it.CatalogItemID]
		if !ok || !meta.exists {
			continue
		}
		if meta.isBundle {
			for _, comp := range cache.bundles[it.CatalogItemID] {
				out[reqKey{comp.ChildCatalogItemID, wh}] += round4(comp.Qty * it.Qty)
			}
			continue
		}
		if !meta.trackStock {
			continue
		}
		out[reqKey{it.CatalogItemID, wh}] += it.Qty
	}
	return out
}

func collectCatalogIDsFromOrders(itemsList ...[]OrderStockItem) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, items := range itemsList {
		for _, it := range items {
			id := strings.TrimSpace(it.CatalogItemID)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

func batchOrderNetIssued(ctx context.Context, q rowsQuerier, orderIDs []string) (map[string]map[reqKey]netEntry, error) {
	orderIDs = uniqueNonEmpty(orderIDs)
	out := make(map[string]map[reqKey]netEntry, len(orderIDs))
	if len(orderIDs) == 0 {
		return out, nil
	}
	clause, args := inClause(1, len(orderIDs))
	for i, id := range orderIDs {
		args[i] = id
	}
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT ref_id::text, catalog_item_id::text, warehouse_id::text, movement_type, qty, total_cost
		FROM inv_stock_movement
		WHERE ref_type='order' AND ref_id IN (%s)
		  AND movement_type IN ('sale_issue','sale_cancel_restore')`, clause), args...)
	if err != nil {
		return nil, appErrsInternal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var orderID, item, wh, mtype string
		var qty, cost float64
		if err := rows.Scan(&orderID, &item, &wh, &mtype, &qty, &cost); err != nil {
			return nil, appErrsInternal(err)
		}
		byKey := out[orderID]
		if byKey == nil {
			byKey = map[reqKey]netEntry{}
			out[orderID] = byKey
		}
		k := reqKey{item, wh}
		e := byKey[k]
		if mtype == MovementSaleIssue {
			e.qty += qty
			e.cost += cost
		} else {
			e.qty -= qty
			e.cost -= cost
		}
		byKey[k] = e
	}
	if err := rows.Err(); err != nil {
		return nil, appErrsInternal(err)
	}
	for _, id := range orderIDs {
		if out[id] == nil {
			out[id] = map[reqKey]netEntry{}
		}
	}
	return out, nil
}

func batchOnHand(ctx context.Context, q rowsQuerier, keys []reqKey) (map[reqKey]float64, error) {
	uniq := make([]reqKey, 0, len(keys))
	seen := map[reqKey]struct{}{}
	for _, k := range keys {
		if k.item == "" || k.warehouse == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		uniq = append(uniq, k)
	}
	out := make(map[reqKey]float64, len(uniq))
	if len(uniq) == 0 {
		return out, nil
	}
	tupleParts := make([]string, len(uniq))
	args := make([]any, 0, len(uniq)*2)
	idx := 1
	for i, k := range uniq {
		tupleParts[i] = fmt.Sprintf("($%d::uuid,$%d::uuid)", idx, idx+1)
		args = append(args, k.item, k.warehouse)
		idx += 2
	}
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT catalog_item_id::text, warehouse_id::text, COALESCE(on_hand, 0)
		FROM inv_stock_balance
		WHERE (catalog_item_id, warehouse_id) IN (%s)`, strings.Join(tupleParts, ",")), args...)
	if err != nil {
		return nil, appErrsInternal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var item, wh string
		var onHand float64
		if err := rows.Scan(&item, &wh, &onHand); err != nil {
			return nil, appErrsInternal(err)
		}
		out[reqKey{item, wh}] = onHand
	}
	if err := rows.Err(); err != nil {
		return nil, appErrsInternal(err)
	}
	for _, k := range uniq {
		if _, ok := out[k]; !ok {
			out[k] = 0
		}
	}
	return out, nil
}

func batchItemNames(ctx context.Context, q rowsQuerier, ids []string) (map[string]string, error) {
	ids = uniqueNonEmpty(ids)
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	clause, args := inClause(1, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT id::text, COALESCE(name, '') FROM business_catalog_item WHERE id IN (%s)`, clause), args...)
	if err != nil {
		return nil, appErrsInternal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, appErrsInternal(err)
		}
		if strings.TrimSpace(name) == "" {
			name = "item"
		}
		out[id] = name
	}
	return out, rows.Err()
}

func batchWarehouseNames(ctx context.Context, q rowsQuerier, ids []string) (map[string]string, error) {
	ids = uniqueNonEmpty(ids)
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	clause, args := inClause(1, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT id::text, COALESCE(name, '') FROM inv_warehouse WHERE id IN (%s)`, clause), args...)
	if err != nil {
		return nil, appErrsInternal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, appErrsInternal(err)
		}
		if strings.TrimSpace(name) == "" {
			name = "gudang"
		}
		out[id] = name
	}
	return out, rows.Err()
}

// backfillBatch preloads SKU, net issued, and on-hand for a large backfill scan.
type backfillBatch struct {
	defaultWarehouse string
	setup            bool
	block            bool
	skuCache         *skuBundleCache
	netByOrder       map[string]map[reqKey]netEntry
	onHand           map[reqKey]float64
	itemNames        map[string]string
	warehouseNames   map[string]string
}

func newBackfillBatch(ctx context.Context, conn *sql.Conn, orders []backfillOrderRow) (*backfillBatch, error) {
	setup, _, block, err := loadSyncSetting(ctx, conn)
	if err != nil {
		return nil, err
	}
	defaultWarehouse, err := defaultWarehouseID(ctx, conn)
	if err != nil {
		return nil, err
	}

	allItems := make([][]OrderStockItem, 0, len(orders))
	orderIDs := make([]string, 0, len(orders))
	for _, o := range orders {
		allItems = append(allItems, o.items)
		orderIDs = append(orderIDs, o.id)
	}
	catalogIDs := collectCatalogIDsFromOrders(allItems...)
	cache := newSkuBundleCache(conn)
	if err := cache.preload(ctx, catalogIDs); err != nil {
		return nil, err
	}
	netByOrder, err := batchOrderNetIssued(ctx, conn, orderIDs)
	if err != nil {
		return nil, err
	}

	b := &backfillBatch{
		defaultWarehouse: defaultWarehouse,
		setup:            setup,
		block:            block,
		skuCache:         cache,
		netByOrder:       netByOrder,
		onHand:           map[reqKey]float64{},
		itemNames:        map[string]string{},
		warehouseNames:   map[string]string{},
	}
	return b, nil
}

func (b *backfillBatch) needsStockSync(orderID, status string, items []OrderStockItem) bool {
	required := map[reqKey]float64{}
	if IsCommittedOrderStatus(status) {
		required = resolveOrderRequirementsWithCache(b.skuCache, items, b.defaultWarehouse)
	}
	netIssued := b.netByOrder[orderID]
	if netIssued == nil {
		netIssued = map[reqKey]netEntry{}
	}
	return orderStockSyncDelta(required, netIssued)
}

func (b *backfillBatch) analyzeShortages(ctx context.Context, conn *sql.Conn, orderID string, items []OrderStockItem) ([]StockShortageLine, error) {
	if !b.setup || !b.block {
		return nil, nil
	}
	required := resolveOrderRequirementsWithCache(b.skuCache, items, b.defaultWarehouse)
	netIssued := b.netByOrder[orderID]
	if netIssued == nil {
		netIssued = map[reqKey]netEntry{}
	}

	needOnHand := make([]reqKey, 0)
	for k, req := range required {
		delta := round4(req - netIssued[k].qty)
		if delta <= epsilon {
			continue
		}
		needOnHand = append(needOnHand, k)
	}
	if err := b.ensureOnHand(ctx, conn, needOnHand); err != nil {
		return nil, err
	}

	itemIDs := make([]string, 0, len(needOnHand))
	whIDs := make([]string, 0, len(needOnHand))
	for _, k := range needOnHand {
		itemIDs = append(itemIDs, k.item)
		whID := k.warehouse
		if whID == "" {
			whID = b.defaultWarehouse
		}
		whIDs = append(whIDs, whID)
	}
	if err := b.ensureNames(ctx, conn, itemIDs, whIDs); err != nil {
		return nil, err
	}

	var shortages []StockShortageLine
	for _, k := range needOnHand {
		req := required[k]
		delta := round4(req - netIssued[k].qty)
		if delta <= epsilon {
			continue
		}
		onHand := b.onHand[k]
		if delta <= onHand+epsilon {
			continue
		}
		whID := k.warehouse
		if whID == "" {
			whID = b.defaultWarehouse
		}
		shortages = append(shortages, StockShortageLine{
			CatalogItemID: k.item,
			ItemName:      b.itemNames[k.item],
			WarehouseID:   whID,
			WarehouseName: b.warehouseNames[whID],
			QtyRequired:   delta,
			QtyAvailable:  onHand,
			QtyShort:      round4(delta - onHand),
		})
	}
	sortStockShortages(shortages)
	return shortages, nil
}

func (b *backfillBatch) ensureOnHand(ctx context.Context, conn *sql.Conn, keys []reqKey) error {
	missing := make([]reqKey, 0)
	for _, k := range keys {
		if _, ok := b.onHand[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	loaded, err := batchOnHand(ctx, conn, missing)
	if err != nil {
		return err
	}
	for k, v := range loaded {
		b.onHand[k] = v
	}
	return nil
}

func (b *backfillBatch) ensureNames(ctx context.Context, conn *sql.Conn, itemIDs, whIDs []string) error {
	missingItems := make([]string, 0)
	for _, id := range uniqueNonEmpty(itemIDs) {
		if _, ok := b.itemNames[id]; !ok {
			missingItems = append(missingItems, id)
		}
	}
	if len(missingItems) > 0 {
		names, err := batchItemNames(ctx, conn, missingItems)
		if err != nil {
			return err
		}
		for k, v := range names {
			b.itemNames[k] = v
		}
		for _, id := range missingItems {
			if b.itemNames[id] == "" {
				b.itemNames[id] = "item"
			}
		}
	}
	missingWh := make([]string, 0)
	for _, id := range uniqueNonEmpty(whIDs) {
		if _, ok := b.warehouseNames[id]; !ok {
			missingWh = append(missingWh, id)
		}
	}
	if len(missingWh) > 0 {
		names, err := batchWarehouseNames(ctx, conn, missingWh)
		if err != nil {
			return err
		}
		for k, v := range names {
			b.warehouseNames[k] = v
		}
		for _, id := range missingWh {
			if b.warehouseNames[id] == "" {
				b.warehouseNames[id] = "gudang"
			}
		}
	}
	return nil
}

// singleOrderBatch lazily batch-loads on-hand and names for one order shortage check.
type singleOrderBatch struct {
	conn             *sql.Conn
	defaultWarehouse string
	onHand           map[reqKey]float64
	itemNames        map[string]string
	warehouseNames   map[string]string
}

func (s *singleOrderBatch) onHandFor(ctx context.Context, keys []reqKey) (map[reqKey]float64, error) {
	missing := make([]reqKey, 0)
	for _, k := range keys {
		if _, ok := s.onHand[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		loaded, err := batchOnHand(ctx, s.conn, missing)
		if err != nil {
			return nil, err
		}
		for k, v := range loaded {
			s.onHand[k] = v
		}
	}
	out := make(map[reqKey]float64, len(keys))
	for _, k := range keys {
		out[k] = s.onHand[k]
	}
	return out, nil
}

func (s *singleOrderBatch) nameForItem(ctx context.Context, id string) (string, error) {
	if n, ok := s.itemNames[id]; ok {
		return n, nil
	}
	names, err := batchItemNames(ctx, s.conn, []string{id})
	if err != nil {
		return "item", err
	}
	n := names[id]
	if n == "" {
		n = "item"
	}
	s.itemNames[id] = n
	return n, nil
}

func (s *singleOrderBatch) nameForWarehouse(ctx context.Context, id string) (string, error) {
	if n, ok := s.warehouseNames[id]; ok {
		return n, nil
	}
	names, err := batchWarehouseNames(ctx, s.conn, []string{id})
	if err != nil {
		return "gudang", err
	}
	n := names[id]
	if n == "" {
		n = "gudang"
	}
	s.warehouseNames[id] = n
	return n, nil
}
