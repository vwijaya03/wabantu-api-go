package ai

import (
	"context"
	"fmt"
	"strings"
)

func catalogStockLinesForItem(catalog []dbCatalogItem, catalogItemID string) (tracked bool, lines []catalogStockLine) {
	for i := range catalog {
		if catalog[i].ID == catalogItemID && catalog[i].StockTracked {
			return true, catalog[i].StockByWarehouse
		}
	}
	return false, nil
}

func maxSingleWarehouseAvail(lines []catalogStockLine) float64 {
	var max float64
	for _, ln := range lines {
		if ln.Available > max {
			max = ln.Available
		}
	}
	return max
}

func formatStockBreakdownLines(lines []catalogStockLine, includeZero bool) []string {
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if !includeZero && ln.Available <= 0 {
			continue
		}
		label := warehouseBuyerLabel(ln.CustomerLabel, ln.WarehouseName)
		if label == "" {
			label = "Gudang"
		}
		out = append(out, fmt.Sprintf("• %s: %s", label, formatStockLabel(ln.Available)))
	}
	return out
}

func formatStockBreakdownBlock(lines []catalogStockLine) string {
	visible := formatStockBreakdownLines(lines, false)
	if len(visible) == 0 {
		return "Stok tersedia: habis"
	}
	var total float64
	for _, ln := range lines {
		total += ln.Available
	}
	body := strings.Join(visible, "\n")
	if len(visible) > 1 {
		body += "\nTotal: " + formatStockLabel(total)
	}
	return "Stok tersedia:\n" + body
}

func resolveOrderWarehouse(lines []catalogStockLine, qty int, preferredWarehouseID string) (warehouseID string, ok bool) {
	if qty < 1 || len(lines) == 0 {
		return "", false
	}
	need := float64(qty)
	if wh := strings.TrimSpace(preferredWarehouseID); wh != "" {
		for _, ln := range lines {
			if ln.WarehouseID == wh && ln.Available >= need {
				return ln.WarehouseID, true
			}
		}
	}
	ordered := append([]catalogStockLine{}, lines...)
	// default first, then display_order (already sorted in enrichCatalogStock)
	for _, ln := range ordered {
		if ln.Available >= need {
			return ln.WarehouseID, true
		}
	}
	return "", false
}

func stockQtyRejectReply(st orderState, lines []catalogStockLine, formal bool) string {
	product := strings.TrimSpace(st.ProductName)
	if product == "" {
		product = "produk ini"
	}
	if len(lines) == 0 || maxSingleWarehouseAvail(lines) <= 0 {
		if formal {
			return fmt.Sprintf("Maaf kak, stok %s saat ini habis. Pesanan tidak bisa dilanjutkan untuk produk ini.", product)
		}
		return fmt.Sprintf("Maaf kak, %s lagi habis nih 😅 Pesannya belum bisa dilanjutkan ya.", product)
	}
	maxAvail := maxSingleWarehouseAvail(lines)
	maxLabel := formatStockLabel(maxAvail)
	breakdown := strings.Join(formatStockBreakdownLines(lines, false), "\n")
	if formal {
		return fmt.Sprintf(
			"Maaf kak, stok %s per gudang:\n%s\nPesanan %d pcs belum bisa — maksimal %s per pesanan. Mohon kurangi jumlahnya.",
			product, breakdown, st.Qty, maxLabel,
		)
	}
	return fmt.Sprintf(
		"Maaf kak, stok %s per gudang:\n%s\nPesanan %d pcs belum bisa — maksimal %s per pesanan. Mau dikurangi dulu ya?",
		product, breakdown, st.Qty, maxLabel,
	)
}

func validateOrderQtyAgainstStock(st orderState, catalog []dbCatalogItem, formal bool) (reject bool, reply string, warehouseID string) {
	if st.Qty < 1 || strings.TrimSpace(st.CatalogItemID) == "" {
		return false, "", ""
	}
	tracked, lines := catalogStockLinesForItem(catalog, st.CatalogItemID)
	if !tracked {
		return false, "", ""
	}
	wh, ok := resolveOrderWarehouse(lines, st.Qty, st.WarehouseID)
	if !ok {
		return true, stockQtyRejectReply(st, lines, formal), ""
	}
	return false, "", wh
}

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

// guardOrderQtyStep returns blocked=true when no single warehouse can fulfill qty.
func guardOrderQtyStep(st orderState, catalog []dbCatalogItem, formal bool, qtyStep string) (orderState, string, bool) {
	reject, reply, wh := validateOrderQtyAgainstStock(st, catalog, formal)
	if reject {
		st.Step = qtyStep
		st.WarehouseID = ""
		return st, reply, true
	}
	if wh != "" {
		st.WarehouseID = wh
	}
	return st, "", false
}
