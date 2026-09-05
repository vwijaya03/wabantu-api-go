package ai

import (
	"context"
	"fmt"
)

// enrichCatalogStock annotates catalog items with per-warehouse available stock when the
// inventory module is active for the tenant. Best-effort: any error leaves the catalog as-is.
func enrichCatalogStock(ctx context.Context, ts tenantScopedQuerier, catalog []dbCatalogItem) {
	if len(catalog) == 0 {
		return
	}
	var setup bool
	if err := ts.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT setup_completed FROM %s ORDER BY created_at LIMIT 1`, ts.T("inv_setting"))).Scan(&setup); err != nil || !setup {
		return
	}
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT s.catalog_item_id::text, w.id::text, COALESCE(w.customer_label, ''), w.name,
		       w.is_default, w.display_order,
		       COALESCE(GREATEST(b.on_hand - b.reserved, 0), 0)
		FROM %s s
		INNER JOIN %s b ON b.catalog_item_id = s.catalog_item_id
		INNER JOIN %s w ON w.id = b.warehouse_id
		WHERE s.track_stock = true AND s.is_bundle = false
		  AND w.deleted_at IS NULL AND w.is_active = true
		ORDER BY s.catalog_item_id, w.is_default DESC, w.display_order, w.name`,
		ts.T("inv_sku"), ts.T("inv_stock_balance"), ts.T("inv_warehouse")))
	if err != nil {
		return
	}
	defer rows.Close()
	byItem := map[string][]catalogStockLine{}
	for rows.Next() {
		var itemID, whID, customerLabel, whName string
		var isDefault bool
		var displayOrder int
		var avail float64
		if rows.Scan(&itemID, &whID, &customerLabel, &whName, &isDefault, &displayOrder, &avail) != nil {
			continue
		}
		if avail <= 0 {
			continue
		}
		byItem[itemID] = append(byItem[itemID], catalogStockLine{
			WarehouseID:   whID,
			WarehouseName: whName,
			CustomerLabel: customerLabel,
			IsDefault:     isDefault,
			DisplayOrder:  displayOrder,
			Available:     avail,
		})
	}
	for i := range catalog {
		lines, ok := byItem[catalog[i].ID]
		if !ok {
			continue
		}
		catalog[i].StockTracked = true
		catalog[i].StockByWarehouse = lines
		var total float64
		for _, ln := range lines {
			total += ln.Available
		}
		catalog[i].StockAvailable = total
	}
}

const (
	// defaultCatalogLoadLimit is the SQL window for lexical catalog match.
	// Do not silently clamp large limits down to 40 — that dropped SKUs
	// (e.g. Nutella) on bigger tenants. Vector hits are fetched by ID separately.
	defaultCatalogLoadLimit = 200
	maxCatalogLoadLimit     = 500
)

func normalizeCatalogLoadLimit(limit int) int {
	if limit < 1 {
		return defaultCatalogLoadLimit
	}
	if limit > maxCatalogLoadLimit {
		return maxCatalogLoadLimit
	}
	return limit
}

func loadActiveCatalog(ctx context.Context, ts tenantScopedQuerier, limit int) ([]dbCatalogItem, error) {
	limit = normalizeCatalogLoadLimit(limit)
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT id::text, external_code, name,
		       COALESCE(sell_price, 0), COALESCE(sell_unit, 'pcs')
		FROM %s
		WHERE deleted_at IS NULL AND is_active = true
		ORDER BY name ASC LIMIT $1`, ts.T("business_catalog_item")), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dbCatalogItem
	for rows.Next() {
		var it dbCatalogItem
		if err := rows.Scan(&it.ID, &it.ExternalCode, &it.Name, &it.SellPrice, &it.SellUnit); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
