package ai

import (
	"context"
	"fmt"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.dev/rlog"
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
		       COALESCE(sell_price, 0), COALESCE(sell_unit, 'pcs'), COALESCE(description, '')
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
		if err := rows.Scan(&it.ID, &it.ExternalCode, &it.Name, &it.SellPrice, &it.SellUnit, &it.Description); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// applyContactTierPrices re-prices the catalog for the buyer: the contact's active price
// type (business_price_type via contact.price_type_id) or the tenant default. Errors are
// logged and leave sell_price (umum) in place so replies never break on pricing.
func applyContactTierPrices(ctx context.Context, ts tenantScopedQuerier, catalog []dbCatalogItem, contactID string) {
	if len(catalog) == 0 {
		return
	}
	pt, cip, ct := ts.T("business_price_type"), ts.T("business_catalog_item_price"), ts.T("contact")
	var tierID, tierCode string
	err := ts.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT t.id::text, t.code FROM %[1]s t
		WHERE t.deleted_at IS NULL AND t.is_active = true
		  AND (t.id = (SELECT c.price_type_id FROM %[2]s c WHERE c.id::text = $1 AND c.deleted_at IS NULL)
		       OR t.is_default = true)
		ORDER BY (t.id = (SELECT c.price_type_id FROM %[2]s c WHERE c.id::text = $1)) IS TRUE DESC,
		         t.is_default DESC, t.display_order
		LIMIT 1`, pt, ct), contactID).Scan(&tierID, &tierCode)
	if err != nil {
		rlog.Debug("AI: no price tier resolved, using sell_price", "err", err)
		return
	}
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`SELECT catalog_item_id::text, price FROM %s WHERE price_type_id = $1::uuid`, cip), tierID)
	if err != nil {
		rlog.Warn("AI: tier price load failed", "err", err)
		return
	}
	defer rows.Close()
	prices := map[string]float64{}
	for rows.Next() {
		var id string
		var p float64
		if rows.Scan(&id, &p) == nil {
			prices[id] = p
		}
	}
	bf.ApplyTierPrices(catalog, prices, tierCode)
}
