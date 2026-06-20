package ai

import (
	"context"
	"fmt"
	"strings"
)

func catalogStockForItem(catalog []dbCatalogItem, catalogItemID string) (tracked bool, avail float64) {
	for i := range catalog {
		if catalog[i].ID == catalogItemID && catalog[i].StockTracked {
			return true, catalog[i].StockAvailable
		}
	}
	return false, 0
}

func stockQtyRejectReply(st orderState, avail float64, formal bool) string {
	product := strings.TrimSpace(st.ProductName)
	if product == "" {
		product = "produk ini"
	}
	label := formatStockLabel(avail)
	if avail <= 0 {
		if formal {
			return fmt.Sprintf("Maaf kak, stok %s saat ini habis. Pesanan tidak bisa dilanjutkan untuk produk ini.", product)
		}
		return fmt.Sprintf("Maaf kak, %s lagi habis nih 😅 Pesannya belum bisa dilanjutkan ya.", product)
	}
	if formal {
		return fmt.Sprintf("Maaf kak, stok %s hanya %s. Mohon kurangi jumlah pesanan maksimal %s.", product, label, label)
	}
	return fmt.Sprintf("Maaf kak, stok %s cuma %s. Mau dikurangi jadi %s dulu ya?", product, label, label)
}

func validateOrderQtyAgainstStock(st orderState, catalog []dbCatalogItem, formal bool) (reject bool, reply string) {
	if st.Qty < 1 || strings.TrimSpace(st.CatalogItemID) == "" {
		return false, ""
	}
	tracked, avail := catalogStockForItem(catalog, st.CatalogItemID)
	if !tracked {
		return false, ""
	}
	if float64(st.Qty) > avail {
		return true, stockQtyRejectReply(st, avail, formal)
	}
	return false, ""
}

func lookupCatalogStockAvailable(ctx context.Context, q tenantQuerier, catalogItemID string) (tracked bool, avail float64) {
	if strings.TrimSpace(catalogItemID) == "" {
		return false, 0
	}
	var setup bool
	if err := q.QueryRowContext(ctx,
		`SELECT setup_completed FROM inv_setting ORDER BY created_at LIMIT 1`).Scan(&setup); err != nil || !setup {
		return false, 0
	}
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(GREATEST(b.on_hand - b.reserved, 0)), 0)
		FROM inv_sku s
		LEFT JOIN inv_stock_balance b ON b.catalog_item_id = s.catalog_item_id
		WHERE s.catalog_item_id = $1::uuid AND s.track_stock = true AND s.is_bundle = false`,
		catalogItemID).Scan(&avail)
	if err != nil {
		return false, 0
	}
	return true, avail
}

func ensureDraftOrderStock(ctx context.Context, q tenantQuerier, st orderState) (reject bool, reply string) {
	if st.Qty < 1 || strings.TrimSpace(st.CatalogItemID) == "" {
		return false, ""
	}
	tracked, avail := lookupCatalogStockAvailable(ctx, q, st.CatalogItemID)
	if !tracked {
		return false, ""
	}
	if float64(st.Qty) > avail {
		return true, stockQtyRejectReply(st, avail, false)
	}
	return false, ""
}

// guardOrderQtyStep returns blocked=true when qty exceeds tracked stock; step reverts to qtyStep.
func guardOrderQtyStep(st orderState, catalog []dbCatalogItem, formal bool, qtyStep string) (orderState, string, bool) {
	if reject, reply := validateOrderQtyAgainstStock(st, catalog, formal); reject {
		st.Step = qtyStep
		return st, reply, true
	}
	return st, "", false
}
