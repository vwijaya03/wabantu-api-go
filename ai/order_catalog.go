package ai

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

type catalogStockLine struct {
	WarehouseID   string
	WarehouseName string
	CustomerLabel string
	IsDefault     bool
	DisplayOrder  int
	Available     float64
}

type dbCatalogItem struct {
	ID           string
	ExternalCode string
	Name         string
	SellPrice    float64
	SellUnit     string
	// Stock fields populated by enrichCatalogStock when the inventory module is
	// set up for the tenant. Default zero values keep non-inventory tenants and
	// unit tests (which build items directly) unaffected.
	StockTracked     bool
	StockAvailable   float64
	StockByWarehouse []catalogStockLine
}

func warehouseBuyerLabel(customerLabel, warehouseName string) string {
	if s := strings.TrimSpace(customerLabel); s != "" {
		return s
	}
	return strings.TrimSpace(warehouseName)
}

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

var (
	postalCodeIDRe = regexp.MustCompile(`\b(\d{5})\b`)
	phoneIDRe      = regexp.MustCompile(`(?:\+62|62|0)8[0-9]{8,11}`)
	colorHintRe    = regexp.MustCompile(`(?i)(warna|color|colour)\s*[:\-]?\s*([a-z]+)`)
)

func loadActiveCatalog(ctx context.Context, ts tenantScopedQuerier, limit int) ([]dbCatalogItem, error) {
	if limit < 1 || limit > 100 {
		limit = 40
	}
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

func matchCatalogItem(userText string, catalog []dbCatalogItem) *dbCatalogItem {
	if len(catalog) == 0 {
		return nil
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return nil
	}

	exclude := catalogExcludeHints(text)
	preferPria := strings.Contains(text, "pria") || strings.Contains(text, "cowok")
	preferAnak := strings.Contains(text, "anak") || strings.Contains(text, "perempuan")

	var best *dbCatalogItem
	var bestScore float64
	for i := range catalog {
		it := &catalog[i]
		nameLower := strings.ToLower(it.Name)
		if catalogItemExcluded(nameLower, exclude) {
			continue
		}
		score := overlapScore(tokens, tokenize(nameLower))
		if nameLower != "" && strings.Contains(text, nameLower) {
			score += 0.35
		}
		for _, tok := range tokens {
			if len(tok) >= 4 && strings.Contains(nameLower, tok) {
				score += 0.08
			}
		}
		score += catalogPhraseBoost(text, nameLower)
		if preferPria {
			if strings.Contains(nameLower, "pria") || strings.Contains(nameLower, "cowok") {
				score += 0.25
			}
			if strings.Contains(nameLower, "perempuan") || strings.Contains(nameLower, "anak perempuan") {
				score -= 0.35
			}
		}
		if preferAnak && strings.Contains(nameLower, "anak") {
			score += 0.15
		}
		if score > bestScore {
			bestScore = score
			best = it
		}
	}
	if bestScore < 0.12 {
		return nil
	}
	return best
}

func catalogExcludeHints(text string) []string {
	if !strings.Contains(text, "bukan") {
		return nil
	}
	var out []string
	for _, hint := range []string{"hello kitty", "mono spot", "de wasa", "abon", "boxer"} {
		if strings.Contains(text, "bukan "+hint) || strings.Contains(text, "bukan "+strings.ReplaceAll(hint, " ", "")) {
			out = append(out, hint)
		}
		if strings.Contains(text, hint) && strings.Contains(text, "bukan") {
			// "boxer pria mono spot bukan hello kitty"
			if hint == "hello kitty" && strings.Contains(text, "bukan") && strings.Contains(text, "hello") {
				out = append(out, hint)
			}
		}
	}
	if strings.Contains(text, "bukan hello") || strings.Contains(text, "bukan hellokitty") {
		out = append(out, "hello kitty")
	}
	return out
}

func catalogItemExcluded(nameLower string, exclude []string) bool {
	for _, ex := range exclude {
		if strings.Contains(nameLower, ex) {
			return true
		}
	}
	return false
}

func catalogPhraseBoost(text, nameLower string) float64 {
	phrases := []string{"mono spot", "hello kitty", "de wasa", "abon sapi"}
	var boost float64
	for _, p := range phrases {
		if strings.Contains(text, p) && strings.Contains(nameLower, p) {
			boost += 0.4
		}
	}
	return boost
}

// tryApplyProductRevision — ganti produk saat checkout ("bukan hello kitty", "mono spot bukan ...").
func tryApplyProductRevision(st *orderState, userText string, catalog []dbCatalogItem) bool {
	if st == nil || len(catalog) == 0 {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	hasSignal := strings.Contains(text, "bukan") || strings.Contains(text, "ubah produk") ||
		strings.Contains(text, "ganti jadi") || strings.Contains(text, "ganti produk") ||
		strings.Contains(text, "mau boxer") || strings.Contains(text, "mau mono") ||
		(strings.Contains(text, "mono spot") && strings.Contains(text, "bukan")) ||
		(strings.Contains(text, "hello kitty") && strings.Contains(text, "bukan")) ||
		(strings.Contains(text, "de wasa") && !strings.Contains(text, "ganti jadi"))
	if !hasSignal {
		return false
	}
	match := matchCatalogItem(userText, catalog)
	if match == nil {
		return false
	}
	prevID := st.CatalogItemID
	applyCatalogMatch(st, match)
	inferVariantFromProductName(st)
	sz, color := parseSizeAndColor(userText)
	if sz != "" {
		st.Size = sz
	}
	if color != "" {
		st.Color = color
	}
	return st.CatalogItemID != prevID || st.ProductName != match.Name
}

func formatCatalogPicker(catalog []dbCatalogItem, max int) string {
	if len(catalog) == 0 {
		return ""
	}
	if max < 1 || max > 8 {
		max = 6
	}
	var lines []string
	for i := 0; i < len(catalog) && i < max; i++ {
		it := catalog[i]
		price := ""
		if it.SellPrice > 0 {
			price = fmt.Sprintf(" — Rp%.0f", it.SellPrice)
		}
		lines = append(lines, fmt.Sprintf("• %s%s", it.Name, price))
	}
	return strings.Join(lines, "\n")
}

func parseSizeAndColor(userText string) (size, color string) {
	text := strings.TrimSpace(userText)
	if m := orderSizeLineRe.FindString(text); m != "" {
		size = strings.ToUpper(m)
	}
	lower := strings.ToLower(text)
	if m := colorHintRe.FindStringSubmatch(lower); len(m) > 2 {
		color = strings.TrimSpace(m[2])
	}
	for _, c := range []string{"hitam", "putih", "biru", "merah", "pink", "cream", "navy", "abu", "coklat", "hijau", "kuning"} {
		if strings.Contains(lower, c) {
			if color == "" {
				color = c
			}
		}
	}
	return size, color
}

func buildVariantLabel(size, color string) string {
	var parts []string
	if size != "" {
		parts = append(parts, "Ukuran: "+size)
	}
	if color != "" {
		parts = append(parts, "Warna: "+color)
	}
	return strings.Join(parts, " | ")
}

func parseRecipientLine(userText string) (name, phone string) {
	text := strings.TrimSpace(userText)
	if m := phoneIDRe.FindString(text); m != "" {
		phone = normalizePhoneID(m)
		name = strings.TrimSpace(strings.ReplaceAll(text, m, ""))
		name = strings.TrimSpace(strings.Trim(name, ",;-"))
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if p := phoneIDRe.FindString(line); p != "" && phone == "" {
			phone = normalizePhoneID(p)
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "nama") {
			name = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		} else if strings.HasPrefix(lower, "hp") || strings.HasPrefix(lower, "no") || strings.HasPrefix(lower, "telp") {
			if p := phoneIDRe.FindString(line); p != "" {
				phone = normalizePhoneID(p)
			}
		} else if phone == "" && phoneIDRe.MatchString(line) {
			phone = normalizePhoneID(phoneIDRe.FindString(line))
		} else if name == "" && len(line) > 2 {
			name = line
		}
	}
	return name, phone
}

func normalizePhoneID(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "+")
	if strings.HasPrefix(p, "62") {
		return "+" + p
	}
	if strings.HasPrefix(p, "0") {
		return "+62" + strings.TrimPrefix(p, "0")
	}
	return p
}

func mergeShippingText(st *orderState, userText string) {
	if st == nil {
		return
	}
	text := strings.TrimSpace(userText)
	if text == "" {
		return
	}
	lower := strings.ToLower(text)

	if st.PostalCode == "" {
		if m := postalCodeIDRe.FindStringSubmatch(text); len(m) > 1 {
			st.PostalCode = m[1]
		}
	}

	name, phone := parseRecipientLine(text)
	if name != "" && st.RecipientName == "" {
		st.RecipientName = name
	}
	if phone != "" && st.RecipientPhone == "" {
		st.RecipientPhone = phone
	}

	// Labelled lines (format resmi Indonesia).
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		low := strings.ToLower(line)
		val := labelValue(line)
		switch {
		case strings.HasPrefix(low, "nama penerima"), strings.HasPrefix(low, "nama"):
			if val != "" {
				st.RecipientName = val
			}
		case strings.HasPrefix(low, "no hp"), strings.HasPrefix(low, "hp"), strings.HasPrefix(low, "telepon"), strings.HasPrefix(low, "wa"):
			if p := phoneIDRe.FindString(val); p != "" {
				st.RecipientPhone = normalizePhoneID(p)
			}
		case strings.HasPrefix(low, "jalan"), strings.HasPrefix(low, "alamat jalan"), strings.HasPrefix(low, "jl"):
			if val != "" {
				st.Street = val
			}
		case strings.HasPrefix(low, "rt/rw"), strings.Contains(low, "rt/rw"):
			parts := strings.SplitN(val, "/", 2)
			if len(parts) == 2 {
				st.RT = strings.TrimSpace(parts[0])
				st.RW = strings.TrimSpace(parts[1])
			} else {
				st.RT = val
			}
		case strings.HasPrefix(low, "rt"):
			st.RT = val
		case strings.HasPrefix(low, "rw"):
			st.RW = val
		case strings.HasPrefix(low, "kel"), strings.HasPrefix(low, "kelurahan"):
			st.Kelurahan = val
		case strings.HasPrefix(low, "kec"), strings.HasPrefix(low, "kecamatan"):
			st.Kecamatan = val
		case strings.HasPrefix(low, "kota"), strings.HasPrefix(low, "kab"):
			st.City = val
		case strings.HasPrefix(low, "prov"), strings.HasPrefix(low, "provinsi"):
			st.Province = val
		case strings.HasPrefix(low, "kode pos"), strings.HasPrefix(low, "pos"):
			if m := postalCodeIDRe.FindString(val); m != "" {
				st.PostalCode = m
			}
		case strings.HasPrefix(low, "negara"):
			st.Country = val
		}
	}

	if st.Street == "" && orderAddrHintRe.MatchString(text) {
		st.Street = strings.TrimSpace(text)
	}
	parseUnstructuredAddress(st, lower, text)
	if st.Country == "" {
		st.Country = "Indonesia"
	}
}

var idCityHints = []struct {
	needle   string
	city     string
	province string
}{
	{"jakarta selatan", "Jakarta Selatan", "DKI Jakarta"},
	{"jaksel", "Jakarta Selatan", "DKI Jakarta"},
	{"jakarta pusat", "Jakarta Pusat", "DKI Jakarta"},
	{"jakarta timur", "Jakarta Timur", "DKI Jakarta"},
	{"jakarta barat", "Jakarta Barat", "DKI Jakarta"},
	{"jakarta utara", "Jakarta Utara", "DKI Jakarta"},
	{"surabaya", "Surabaya", "Jawa Timur"},
	{"bandung", "Bandung", "Jawa Barat"},
	{"yogyakarta", "Yogyakarta", "DI Yogyakarta"},
	{"semarang", "Semarang", "Jawa Tengah"},
	{"medan", "Medan", "Sumatera Utara"},
	{"bekasi", "Bekasi", "Jawa Barat"},
	{"tangerang selatan", "Tangerang Selatan", "Banten"},
	{"tangerang", "Tangerang", "Banten"},
	{"depok", "Depok", "Jawa Barat"},
}

func parseUnstructuredAddress(st *orderState, lower, raw string) {
	if st == nil {
		return
	}
	for _, h := range idCityHints {
		if strings.Contains(lower, h.needle) {
			if st.City == "" {
				st.City = h.city
			}
			if st.Province == "" {
				st.Province = h.province
			}
			break
		}
	}
	// "Jl X, Jakarta Selatan" — keep street before comma when whole text was dumped to Street.
	if st.Street != "" && strings.Contains(st.Street, ",") {
		parts := strings.SplitN(st.Street, ",", 2)
		if len(parts) == 2 && orderAddrHintRe.MatchString(parts[0]) {
			st.Street = strings.TrimSpace(parts[0])
			tail := strings.TrimSpace(strings.ToLower(parts[1]))
			for _, h := range idCityHints {
				if strings.Contains(tail, h.needle) {
					if st.City == "" {
						st.City = h.city
					}
					if st.Province == "" {
						st.Province = h.province
					}
					break
				}
			}
		}
	}
	_ = raw
}

func labelValue(line string) string {
	if i := strings.Index(line, ":"); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return strings.TrimSpace(line)
}

func (st orderState) shippingComplete() bool {
	if st.RecipientName == "" || st.RecipientPhone == "" {
		return false
	}
	if st.Street == "" || st.City == "" || st.Province == "" {
		return false
	}
	if len(st.PostalCode) != 5 {
		return false
	}
	return postalCodeIDRe.MatchString(st.PostalCode)
}

func (st orderState) productComplete() bool {
	if st.hasMultiItems() {
		return st.structuredLinesReady()
	}
	return strings.TrimSpace(st.ProductName) != "" || strings.TrimSpace(st.CatalogItemID) != ""
}

// catalogItemNeedsVariant — apparel/ukuran; makanan & produk tanpa varian dilewati.
func catalogItemNeedsVariant(it *dbCatalogItem) bool {
	if it == nil {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(it.Name))
	if name == "" {
		return false
	}
	if extractSizeFromProductName(it.Name) != "" {
		return true
	}
	for _, kw := range []string{
		"celana", "boxer", "jeans", "baju", "kaos", "dress", "rok", "kemeja",
		"jaket", "hoodie", "dalam", "highwaist", "hotpants", "skinny",
	} {
		if strings.Contains(name, kw) {
			return true
		}
	}
	return false
}

func (st orderState) variantComplete() bool {
	it := &dbCatalogItem{Name: st.ProductName, ExternalCode: st.ExternalCode}
	if !catalogItemNeedsVariant(it) {
		return true
	}
	return st.Size != "" || st.Color != ""
}

func applyCatalogMatch(st *orderState, it *dbCatalogItem) {
	if st == nil || it == nil {
		return
	}
	st.CatalogItemID = it.ID
	st.ExternalCode = it.ExternalCode
	st.ProductName = it.Name
	st.UnitPrice = it.SellPrice
	st.SellUnit = it.SellUnit
	if st.ProductName == "" {
		st.ProductName = it.Name
	}
	inferVariantFromProductName(st)
}

func catalogConfirmLine(st orderState) string {
	summary := formatOrderSummary(st)
	if summary != "" {
		return summary
	}
	if st.ProductName == "" {
		return ""
	}
	it := &dbCatalogItem{
		Name:       st.ProductName,
		SellPrice:  st.UnitPrice,
		SellUnit:   st.SellUnit,
		ExternalCode: st.ExternalCode,
	}
	return strings.TrimSpace("Produk:\n" + st.ProductName + "\n\nHarga:\n" + formatCatalogPrice(it))
}

func missingOrderDataPrompt(st orderState, tmpl orderFlowTemplates) string {
	st = normalizeOrderState(st)
	if st.hasMultiItems() {
		if !st.structuredLinesReady() {
			return tmpl.AskVariant
		}
	} else {
		if !st.productComplete() {
			return tmpl.AskProduct
		}
		if !st.variantComplete() {
			return tmpl.AskVariant
		}
		if st.Qty < 1 {
			return tmpl.AskQty
		}
	}
	if strings.TrimSpace(st.RecipientName) == "" || strings.TrimSpace(st.RecipientPhone) == "" {
		return tmpl.AskRecipient
	}
	if !st.shippingComplete() {
		return tmpl.ClarifyAddress
	}
	return ""
}
