package buyerflow

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

func formatCatalogPrice(it *CatalogItem) string {
	if it == nil || it.SellPrice <= 0 {
		return "Harga belum di-set"
	}
	info := parseCatalogPriceInfo(it)
	if info.IsPackListing {
		return fmt.Sprintf("Rp%.0f/paket (isi %d pcs)", info.ListPrice, info.PackCount)
	}
	return fmt.Sprintf("Rp%.0f/%s", info.ListPrice, info.UnitLabel)
}

func formatMoney(amount float64) string {
	if amount <= 0 {
		return "Rp0"
	}
	return fmt.Sprintf("Rp%.0f", amount)
}

func inferProductFamily(it CatalogItem) string {
	if cat := inferCatalogCategory(it); cat != "" {
		return cat
	}
	code := strings.TrimSpace(it.ExternalCode)
	if code != "" && isValidCategoryLabel(code) {
		family := codeSizeSuffixRe.ReplaceAllString(code, "")
		if family != "" && family != code && isValidCategoryLabel(family) {
			return humanizeFamilyLabel(family)
		}
	}
	name := catalogQtyPrefixRe.ReplaceAllString(it.Name, "")
	name = productSizeSuffixRe.ReplaceAllString(name, "")
	name = strings.TrimSpace(name)
	if isValidCategoryLabel(name) {
		return name
	}
	return ""
}

func isValidCategoryLabel(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 3 {
		return false
	}
	if regexp.MustCompile(`^\d+$`).MatchString(s) {
		return false
	}
	if regexp.MustCompile(`(?i)^(xs|s|m|l|xl|xxl),`).MatchString(s) {
		return false
	}
	if strings.Contains(strings.ToLower(s), ",acak") {
		return false
	}
	return true
}

func inferCatalogCategory(it CatalogItem) string {
	name := strings.ToLower(it.Name)
	switch {
	case strings.Contains(name, "abon") || strings.Contains(name, "keripik") || strings.Contains(name, "snack"):
		return "Makanan"
	case strings.Contains(name, "anak perempuan") || (strings.Contains(name, "perempuan") && strings.Contains(name, "anak")):
		return "Anak Perempuan"
	case strings.Contains(name, "anak"):
		return "Anak"
	case strings.Contains(name, "pria") || strings.Contains(name, "cowok"):
		return "Pria Dewasa"
	case strings.Contains(name, "wanita") || strings.Contains(name, "cewek"):
		return "Wanita"
	case strings.Contains(name, "celana") || strings.Contains(name, "jeans") || strings.Contains(name, "boxer"):
		return "Pakaian"
	}
	return ""
}

func humanizeFamilyLabel(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	return strings.TrimSpace(s)
}

func extractCatalogCategories(catalog []CatalogItem) []string {
	seen := make(map[string]struct{})
	var cats []string
	for _, it := range catalog {
		cat := inferCatalogCategory(it)
		if cat == "" {
			cat = inferProductFamily(it)
		}
		if cat == "" || !isValidCategoryLabel(cat) {
			continue
		}
		key := strings.ToLower(cat)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	return cats
}

func shortDisplayName(name string) string {
	name = catalogQtyPrefixRe.ReplaceAllString(name, "")
	if len(name) > 48 {
		return strings.TrimSpace(name[:45]) + "..."
	}
	return strings.TrimSpace(name)
}

func groupCatalogByCategory(catalog []CatalogItem) map[string][]CatalogItem {
	out := make(map[string][]CatalogItem)
	seen := make(map[string]struct{})
	for _, it := range catalog {
		cat := inferCatalogCategory(it)
		if cat == "" {
			cat = "Lainnya"
		}
		family := strings.ToLower(inferProductFamily(it))
		if family != "" {
			key := cat + "|" + family
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
		}
		out[cat] = append(out[cat], it)
	}
	return out
}

func categoryEmoji(cat string) string {
	switch strings.ToLower(cat) {
	case "makanan":
		return "🍖"
	case "anak perempuan", "anak":
		return "👧"
	case "pria dewasa":
		return "👨"
	case "wanita":
		return "👩"
	case "pakaian":
		return "👕"
	default:
		return "•"
	}
}

func pickFeaturedCatalogItems(catalog []CatalogItem, max int) []CatalogItem {
	if max < 1 {
		max = 8
	}
	seen := make(map[string]struct{})
	var out []CatalogItem
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

func formatTopProductLine(it *CatalogItem, num int) string {
	if it == nil {
		return ""
	}
	return fmt.Sprintf("%d. %s\n%s", num, it.Name, formatCatalogPrice(it))
}

func formatFeaturedProductsBody(items []CatalogItem) string {
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

func formatOrderSummary(st OrderState) string {
	st = normalizeOrderState(st)
	if st.HasMultiItems() {
		return formatMultiOrderSummary(st)
	}
	if !st.ProductComplete() {
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

	it := &CatalogItem{Name: st.ProductName, ExternalCode: st.ExternalCode}
	needsVariant := catalogItemNeedsVariant(it)

	var b strings.Builder
	b.WriteString("🛒 Ringkasan Pesanan\n\n")
	b.WriteString("Produk:\n")
	b.WriteString(st.ProductName + "\n\n")
	b.WriteString(fmt.Sprintf("Qty: %d\n", qty))
	b.WriteString(fmt.Sprintf("Harga: %s\n\n", priceLine))
	if needsVariant {
		if variant := buildVariantLabel(st.Size, st.Color); variant != "" {
			b.WriteString(variant + "\n\n")
		} else if sz := extractSizeFromProductName(st.ProductName); sz != "" && st.Size == "" {
			b.WriteString("Ukuran: " + sz + "\n\n")
		}
	}
	if st.UnitPrice > 0 {
		b.WriteString("Subtotal:\n")
		b.WriteString(formatMoney(subtotal))
		b.WriteString("\n\nBelum termasuk ongkir.")
	}
	return strings.TrimSpace(b.String())
}

func formatMultiOrderSummary(st OrderState) string {
	if !st.StructuredLinesReady() {
		return ""
	}
	var b strings.Builder
	b.WriteString("🛒 Ringkasan Pesanan\n\n")
	var subtotal float64
	for i, ln := range st.Items {
		qty := ln.Qty
		if qty < 1 {
			qty = 1
		}
		unit := ln.SellUnit
		if unit == "" {
			unit = "pcs"
		}
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, ln.ProductName))
		b.WriteString(fmt.Sprintf("   Qty: %d", qty))
		if variant := buildVariantLabel(ln.Size, ln.Color); variant != "" {
			b.WriteString(" · " + variant)
		}
		b.WriteString("\n")
		if ln.UnitPrice > 0 {
			lineSub := float64(qty) * ln.UnitPrice
			subtotal += lineSub
			b.WriteString(fmt.Sprintf("   %s/%s → %s\n", formatMoney(ln.UnitPrice), unit, formatMoney(lineSub)))
		}
		b.WriteString("\n")
	}
	if subtotal > 0 {
		b.WriteString("Subtotal:\n")
		b.WriteString(formatMoney(subtotal))
		b.WriteString("\n\nBelum termasuk ongkir.")
	}
	return strings.TrimSpace(b.String())
}

func suggestRelatedProducts(st OrderState, catalog []CatalogItem, max int) []CatalogItem {
	if max < 1 || st.CatalogItemID == "" || len(catalog) == 0 {
		return nil
	}
	baseTokens := tokenize(st.ProductName)
	type scored struct {
		it    CatalogItem
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
	var out []CatalogItem
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

func formatUpsellBlock(st OrderState, catalog []CatalogItem) string {
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

func orderCompleteMessage(st OrderState, tmpl orderFlowTemplates) string {
	return orderCompleteMessageWithRef("", st, tmpl)
}

func orderCompleteMessageWithRef(orderID string, st OrderState, tmpl orderFlowTemplates) string {
	summary := formatOrderSummary(st)
	complete := tmpl.Complete
	if ref := FormatOrderNumber(orderID); ref != "" {
		complete = fmt.Sprintf("Nomor pesanan: %s\n\n%s", ref, complete)
	}
	if summary == "" {
		return complete
	}
	return summary + "\n\n" + complete
}
