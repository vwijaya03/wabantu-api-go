package ai

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	productSizeSuffixRe = regexp.MustCompile(`(?i)\s*-\s*(XS|S|M|L|XL|XXL|XXXL|3XL|4XL|5XL|\d{2,3})\s*$`)
	catalogQtyPrefixRe  = regexp.MustCompile(`(?i)^\d+\s*pcs\s+`)
	codeSizeSuffixRe    = regexp.MustCompile(`(?i)-(XS|S|M|L|XL|XXL|XXXL|3XL|4XL|5XL|\d{2,3})$`)
)

func formatCatalogPrice(it *dbCatalogItem) string {
	if it == nil || it.SellPrice <= 0 {
		return "Harga belum di-set"
	}
	unit := it.SellUnit
	if unit == "" {
		unit = "pcs"
	}
	return fmt.Sprintf("Rp%.0f/%s", it.SellPrice, unit)
}

func formatMoney(amount float64) string {
	if amount <= 0 {
		return "Rp0"
	}
	return fmt.Sprintf("Rp%.0f", amount)
}

func inferProductFamily(it dbCatalogItem) string {
	code := strings.TrimSpace(it.ExternalCode)
	if code != "" {
		family := codeSizeSuffixRe.ReplaceAllString(code, "")
		if family != "" && family != code {
			return humanizeFamilyLabel(family)
		}
	}
	name := catalogQtyPrefixRe.ReplaceAllString(it.Name, "")
	name = productSizeSuffixRe.ReplaceAllString(name, "")
	return strings.TrimSpace(name)
}

func humanizeFamilyLabel(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	return strings.TrimSpace(s)
}

func extractCatalogCategories(catalog []dbCatalogItem) []string {
	seen := make(map[string]struct{})
	var cats []string
	for _, it := range catalog {
		family := inferProductFamily(it)
		if family == "" {
			continue
		}
		key := strings.ToLower(family)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cats = append(cats, family)
	}
	sort.Strings(cats)
	return cats
}

func pickFeaturedCatalogItems(catalog []dbCatalogItem, max int) []dbCatalogItem {
	if max < 1 {
		max = 8
	}
	seen := make(map[string]struct{})
	var out []dbCatalogItem
	for _, it := range catalog {
		family := strings.ToLower(inferProductFamily(it))
		if family == "" {
			continue
		}
		if _, ok := seen[family]; ok {
			continue
		}
		seen[family] = struct{}{}
		out = append(out, it)
		if len(out) >= max {
			break
		}
	}
	return out
}

func formatCategoryList(categories []string, max int) string {
	if max < 1 {
		max = 8
	}
	var lines []string
	for i, c := range categories {
		if i >= max {
			lines = append(lines, fmt.Sprintf("…dan %d kategori lainnya", len(categories)-max))
			break
		}
		lines = append(lines, "• "+c)
	}
	return strings.Join(lines, "\n")
}

func formatTopProductLine(it *dbCatalogItem, num int) string {
	if it == nil {
		return ""
	}
	return fmt.Sprintf("%d. %s\n%s", num, it.Name, formatCatalogPrice(it))
}

func formatFeaturedProductsBody(items []dbCatalogItem) string {
	var lines []string
	for i := range items {
		lines = append(lines, formatTopProductLine(&items[i], i+1))
	}
	return strings.Join(lines, "\n\n")
}

func extractSizeFromProductName(name string) string {
	if m := productSizeSuffixRe.FindStringSubmatch(name); len(m) > 1 {
		return strings.ToUpper(strings.TrimSpace(m[1]))
	}
	return ""
}

// IsOrderTotalRequest — pelanggan minta ringkasan/total pesanan.
func IsOrderTotalRequest(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if strings.Contains(text, "totalan") || strings.Contains(text, "subtotal") {
		return true
	}
	if strings.Contains(text, "total") || strings.Contains(text, "tagihan") {
		return true
	}
	return strings.Contains(text, "berapa") &&
		(strings.Contains(text, "semua") || strings.Contains(text, "bayar") || strings.Contains(text, "harga"))
}

func formatOrderSummary(st orderState) string {
	st = normalizeOrderState(st)
	if !st.productComplete() {
		return ""
	}
	qty := st.Qty
	if qty < 1 {
		qty = 1
	}
	unit := st.SellUnit
	if unit == "" {
		unit = "pcs"
	}
	priceLine := "belum di-set"
	if st.UnitPrice > 0 {
		priceLine = fmt.Sprintf("%s/%s", formatMoney(st.UnitPrice), unit)
	}
	subtotal := float64(qty) * st.UnitPrice

	var b strings.Builder
	b.WriteString("Ringkasan Pesanan\n\n")
	b.WriteString("Produk:\n")
	b.WriteString(st.ProductName + "\n\n")
	b.WriteString(fmt.Sprintf("Qty: %d\n", qty))
	b.WriteString(fmt.Sprintf("Harga: %s\n\n", priceLine))
	if variant := buildVariantLabel(st.Size, st.Color); variant != "" {
		b.WriteString(variant + "\n\n")
	} else if sz := extractSizeFromProductName(st.ProductName); sz != "" && st.Size == "" {
		b.WriteString("Ukuran: " + sz + "\n\n")
	}
	if st.UnitPrice > 0 {
		b.WriteString("Subtotal:\n")
		b.WriteString(formatMoney(subtotal))
	}
	return strings.TrimSpace(b.String())
}

func suggestRelatedProducts(st orderState, catalog []dbCatalogItem, max int) []dbCatalogItem {
	if max < 1 || st.CatalogItemID == "" || len(catalog) == 0 {
		return nil
	}
	baseTokens := tokenize(st.ProductName)
	type scored struct {
		it    dbCatalogItem
		score float64
	}
	var picks []scored
	for _, it := range catalog {
		if it.ID == st.CatalogItemID {
			continue
		}
		score := overlapScore(baseTokens, tokenize(it.Name))
		if score < 0.12 {
			continue
		}
		picks = append(picks, scored{it: it, score: score})
	}
	sort.Slice(picks, func(i, j int) bool { return picks[i].score > picks[j].score })

	seen := make(map[string]struct{})
	var out []dbCatalogItem
	for _, p := range picks {
		family := strings.ToLower(inferProductFamily(p.it))
		if _, ok := seen[family]; ok {
			continue
		}
		seen[family] = struct{}{}
		out = append(out, p.it)
		if len(out) >= max {
			break
		}
	}
	return out
}

func formatUpsellBlock(st orderState, catalog []dbCatalogItem) string {
	suggestions := suggestRelatedProducts(st, catalog, 2)
	if len(suggestions) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "💡 Rekomendasi tambahan (tidak wajib):")
	for i, it := range suggestions {
		lines = append(lines, fmt.Sprintf("%d. %s\n%s", i+1, it.Name, formatCatalogPrice(&it)))
	}
	return strings.Join(lines, "\n\n")
}

func orderCompleteMessage(st orderState, tmpl orderFlowTemplates) string {
	summary := formatOrderSummary(st)
	if summary == "" {
		return tmpl.Complete
	}
	return summary + "\n\n" + tmpl.Complete
}
