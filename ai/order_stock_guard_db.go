package ai

import (
	"context"
	"fmt"
	"strings"
)

func lookupCatalogStockLines(ctx context.Context, ts tenantScopedQuerier, catalogItemID string) (tracked bool, lines []catalogStockLine) {
	if strings.TrimSpace(catalogItemID) == "" {
		return false, nil
	}
	var setup bool
	if err := ts.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT setup_completed FROM %s ORDER BY created_at LIMIT 1`, ts.T("inv_setting"))).Scan(&setup); err != nil || !setup {
		return false, nil
	}
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT w.id::text, COALESCE(w.customer_label, ''), w.name, w.is_default, w.display_order,
		       COALESCE(GREATEST(b.on_hand - b.reserved, 0), 0)
		FROM %s s
		INNER JOIN %s b ON b.catalog_item_id = s.catalog_item_id
		INNER JOIN %s w ON w.id = b.warehouse_id
		WHERE s.catalog_item_id = $1::uuid AND s.track_stock = true AND s.is_bundle = false
		  AND w.deleted_at IS NULL AND w.is_active = true
		ORDER BY w.is_default DESC, w.display_order, w.name`,
		ts.T("inv_sku"), ts.T("inv_stock_balance"), ts.T("inv_warehouse")), catalogItemID)
	if err != nil {
		return false, nil
	}
	defer rows.Close()
	for rows.Next() {
		var whID, customerLabel, whName string
		var isDefault bool
		var displayOrder int
		var avail float64
		if rows.Scan(&whID, &customerLabel, &whName, &isDefault, &displayOrder, &avail) != nil {
			continue
		}
		if avail <= 0 {
			continue
		}
		lines = append(lines, catalogStockLine{
			WarehouseID:   whID,
			WarehouseName: whName,
			CustomerLabel: customerLabel,
			IsDefault:     isDefault,
			DisplayOrder:  displayOrder,
			Available:     avail,
		})
	}
	if len(lines) == 0 {
		return true, nil
	}
	return true, lines
}

func ensureDraftOrderStock(ctx context.Context, ts tenantScopedQuerier, st orderState) (reject bool, reply string, warehouseID string) {
	if st.Qty < 1 || strings.TrimSpace(st.CatalogItemID) == "" {
		return false, "", ""
	}
	tracked, lines := lookupCatalogStockLines(ctx, ts, st.CatalogItemID)
	if !tracked {
		return false, "", ""
	}
	wh, ok := resolveOrderWarehouse(lines, st.Qty, st.WarehouseID)
	if !ok {
		return true, stockQtyRejectReply(st, lines, false), ""
	}
	return false, "", wh
}
