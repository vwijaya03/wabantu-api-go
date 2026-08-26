package buyerflow

import (
	"fmt"
	"strings"
)

func CatalogStockLinesForItem(catalog []CatalogItem, catalogItemID string) (tracked bool, lines []CatalogStockLine) {
	for i := range catalog {
		if catalog[i].ID == catalogItemID && catalog[i].StockTracked {
			return true, catalog[i].StockByWarehouse
		}
	}
	return false, nil
}

func maxSingleWarehouseAvail(lines []CatalogStockLine) float64 {
	var max float64
	for _, ln := range lines {
		if ln.Available > max {
			max = ln.Available
		}
	}
	return max
}

func formatStockBreakdownLines(lines []CatalogStockLine, includeZero bool) []string {
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

func formatStockBreakdownBlock(lines []CatalogStockLine) string {
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

func resolveOrderWarehouse(lines []CatalogStockLine, qty int, preferredWarehouseID string) (warehouseID string, ok bool) {
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
	ordered := append([]CatalogStockLine{}, lines...)
	// default first, then display_order (already sorted in enrichCatalogStock)
	for _, ln := range ordered {
		if ln.Available >= need {
			return ln.WarehouseID, true
		}
	}
	return "", false
}

func stockQtyRejectReply(st OrderState, lines []CatalogStockLine, formal bool) string {
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

func validateOrderQtyAgainstStock(st OrderState, catalog []CatalogItem, formal bool) (reject bool, reply string, warehouseID string) {
	if st.Qty < 1 || strings.TrimSpace(st.CatalogItemID) == "" {
		return false, "", ""
	}
	tracked, lines := CatalogStockLinesForItem(catalog, st.CatalogItemID)
	if !tracked {
		return false, "", ""
	}
	wh, ok := resolveOrderWarehouse(lines, st.Qty, st.WarehouseID)
	if !ok {
		return true, stockQtyRejectReply(st, lines, formal), ""
	}
	return false, "", wh
}

// guardOrderQtyStep returns blocked=true when no single warehouse can fulfill qty.
func guardOrderQtyStep(st OrderState, catalog []CatalogItem, formal bool, qtyStep string) (OrderState, string, bool) {
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
