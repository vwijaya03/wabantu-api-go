package buyerflow

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"encore.app/wabantu/shared/retrieval"
)

func warehouseBuyerLabel(customerLabel, warehouseName string) string {
	if s := strings.TrimSpace(customerLabel); s != "" {
		return s
	}
	return strings.TrimSpace(warehouseName)
}

var (
	postalCodeIDRe = regexp.MustCompile(`\b(\d{5})\b`)
	phoneIDRe      = regexp.MustCompile(`(?:\+62|62|0)8[0-9]{8,11}`)
	colorHintRe    = regexp.MustCompile(`(?i)(warna|color|colour)\s*[:\-]?\s*([a-z]+)`)
	explicitWeightInTextRe = regexp.MustCompile(`(?i)(?:^|\s)(\d+(?:[.,]\d+)?)\s*(?:g|gr|gram|kg)(?:\s|$|[,.])`)
)

func isWeightUnit(tok string) bool {
	switch tok {
	case "g", "gr", "gram", "kg":
		return true
	}
	return false
}

func explicitWeightGramsInText(userText string) []int {
	text := strings.ToLower(strings.TrimSpace(userText))
	var out []int
	seen := map[int]struct{}{}
	add := func(n int) {
		if n <= 0 {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	for _, m := range explicitWeightInTextRe.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		val, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
		if err != nil || val <= 0 {
			continue
		}
		grams := int(val)
		if strings.Contains(m[0], "kg") {
			grams = int(val * 1000)
		}
		add(grams)
	}
	toks := tokenize(text)
	for i := 0; i+1 < len(toks); i++ {
		if !isWeightUnit(toks[i+1]) {
			continue
		}
		if n, err := strconv.Atoi(toks[i]); err == nil {
			add(n)
		}
	}
	return out
}

// catalogItemMatchesExplicitWeight — "abon sapi 125 gr" must not bind to Abon Sapi 500G.
func catalogItemMatchesExplicitWeight(userText string, item *CatalogItem) bool {
	if item == nil {
		return true
	}
	weights := explicitWeightGramsInText(userText)
	if len(weights) == 0 {
		return true
	}
	nameLower := strings.ToLower(item.Name)
	for _, w := range weights {
		if strings.Contains(nameLower, strconv.Itoa(w)) {
			return true
		}
	}
	return false
}

func filterCatalogMatchByExplicitWeight(userText string, match *CatalogItem) *CatalogItem {
	if match == nil || catalogItemMatchesExplicitWeight(userText, match) {
		return match
	}
	return nil
}

func matchCatalogItem(userText string, catalog []CatalogItem) *CatalogItem {
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

	var best *CatalogItem
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
		return filterCatalogMatchByExplicitWeight(userText, matchCatalogItemFuzzy(userText, catalog))
	}
	return filterCatalogMatchByExplicitWeight(userText, best)
}

// matchCatalogItemFuzzy — typo singkat (mis. "cadburi" → Cadbury) bila lexical miss.
func matchCatalogItemFuzzy(userText string, catalog []CatalogItem) *CatalogItem {
	text := strings.ToLower(strings.TrimSpace(userText))
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return nil
	}
	exclude := catalogExcludeHints(text)
	var best *CatalogItem
	var bestScore float64
	for i := range catalog {
		it := &catalog[i]
		nameLower := strings.ToLower(it.Name)
		if catalogItemExcluded(nameLower, exclude) {
			continue
		}
		score := fuzzyNameTokenScore(tokens, tokenize(nameLower))
		if score > bestScore {
			bestScore = score
			best = it
		}
	}
	if bestScore < 0.18 {
		return nil
	}
	return filterCatalogMatchByExplicitWeight(userText, best)
}

func fuzzyNameTokenScore(userTokens, nameTokens []string) float64 {
	var score float64
	for _, ut := range userTokens {
		if len(ut) < 4 || isFuzzyStopToken(ut) {
			continue
		}
		for _, nt := range nameTokens {
			if len(nt) < 4 {
				continue
			}
			if fuzzyTokenPrefixMatch(ut, nt) {
				score += 0.22
			}
		}
	}
	return score
}

func isFuzzyStopToken(tok string) bool {
	switch tok {
	case "mau", "pesen", "pesan", "beli", "order", "pcs", "paket", "bukan", "woi", "dong", "kak":
		return true
	}
	return false
}

func fuzzyTokenPrefixMatch(a, b string) bool {
	if a == b {
		return true
	}
	n := 4
	if len(a) < n || len(b) < n {
		return false
	}
	return strings.HasPrefix(a, b[:n]) || strings.HasPrefix(b, a[:n])
}

func resolveOrderProductMatch(userText string, history []Message, catalog []CatalogItem, vctx *CatalogVectorContext) *CatalogItem {
	if unique := uniqueBrandSKUFromText(userText, catalog); unique != nil {
		return filterCatalogMatchByExplicitWeight(userText, unique)
	}
	if unique := uniqueSizedSKUFromText(userText, catalog); unique != nil {
		return filterCatalogMatchByExplicitWeight(userText, unique)
	}
	if lexicalBrandAmbiguous(userText, catalog) {
		return nil
	}
	if m := matchCatalogItem(userText, catalog); m != nil {
		return m
	}
	if vctx != nil && len(vctx.Hits) > 0 {
		if m := MatchCatalogItemSemantic(userText, catalog, vctx.Hits); m != nil {
			return filterCatalogMatchByExplicitWeight(userText, m)
		}
		if orderSemanticAmbiguous(vctx) {
			return nil
		}
	}
	if m := matchCatalogFromRecentOutbound(history, catalog); m != nil {
		return filterCatalogMatchByExplicitWeight(userText, m)
	}
	return nil
}

func orderSemanticAmbiguous(vctx *CatalogVectorContext) bool {
	return vctx != nil && len(vctx.Hits) >= 2 && CatalogSemanticAmbiguous(vctx.Hits)
}

func matchCatalogLine(raw string, catalog []CatalogItem, vctx *CatalogVectorContext) *CatalogItem {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	if m := resolveOrderProductMatch(text, nil, catalog, vctx); m != nil {
		return m
	}
	cleaned := StripOrderSizeTokens(text)
	cleaned = strings.TrimSpace(strings.TrimRight(cleaned, "ya"))
	return resolveOrderProductMatch(cleaned, nil, catalog, vctx)
}

// orderVectorVariantPickerReply — klarifikasi varian saat vector hits ambiguous di order flow.
func orderVectorVariantPickerReply(formal bool, userText string, catalog []CatalogItem, vctx *CatalogVectorContext) (string, bool) {
	if vctx == nil || len(vctx.Hits) == 0 {
		return "", false
	}
	brand := brandTokenFromText(userText, catalog)
	if brand != "" {
		ids := catalogVectorHitsEntryIDs(vctx.Hits)
		if reply := buildBrandVariantListFromVectorHits(formal, brand, catalog, ids, 10); reply != "" {
			return reply, true
		}
	}
	byID := map[string]CatalogItem{}
	for _, it := range catalog {
		byID[it.ID] = it
	}
	var items []CatalogItem
	seen := map[string]struct{}{}
	for _, h := range sortedHitsByScore(vctx.Hits) {
		id := retrieval.EntryIDFromHit(h)
		it, ok := byID[id]
		if !ok {
			continue
		}
		if _, dup := seen[it.ID]; dup {
			continue
		}
		seen[it.ID] = struct{}{}
		items = append(items, it)
	}
	if len(items) < 2 {
		return "", false
	}
	token := brand
	if token == "" {
		token = "produk"
	}
	return formatBrandVariantListBody(formal, token, items, 6), true
}

func catalogExcludeHints(text string) []string {
	lower := strings.ToLower(strings.TrimSpace(text))
	var out []string
	if strings.Contains(lower, "bukan") {
		for _, hint := range []string{"hello kitty", "mono spot", "de wasa", "abon", "boxer"} {
			if strings.Contains(lower, "bukan "+hint) || strings.Contains(lower, "bukan "+strings.ReplaceAll(hint, " ", "")) {
				out = append(out, hint)
			}
			if hint == "hello kitty" && strings.Contains(lower, "bukan") && strings.Contains(lower, "hello") {
				out = append(out, hint)
			}
		}
		if strings.Contains(lower, "bukan hello") || strings.Contains(lower, "bukan hellokitty") {
			out = append(out, "hello kitty")
		}
		for _, prefix := range []string{"bukan "} {
			idx := strings.Index(lower, prefix)
			if idx < 0 {
				continue
			}
			rest := strings.TrimSpace(lower[idx+len(prefix):])
			for _, stop := range []string{" wo", " ya", " kak", " dong", " woi", ",", "?", "!", " tapi", " yang"} {
				if j := strings.Index(rest, stop); j > 0 {
					rest = strings.TrimSpace(rest[:j])
				}
			}
			if rest != "" && len(rest) >= 3 {
				out = append(out, rest)
			}
		}
	}
	for _, prefix := range []string{"selain ", "kecuali "} {
		idx := strings.Index(lower, prefix)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(lower[idx+len(prefix):])
		for _, stop := range []string{" ada ", " list", " produk", " barang", "?", ",", " yang"} {
			if j := strings.Index(rest, stop); j > 0 {
				rest = strings.TrimSpace(rest[:j])
			}
		}
		if rest != "" {
			out = append(out, rest)
		}
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
func tryApplyProductRevision(st *OrderState, userText string, catalog []CatalogItem, vctx *CatalogVectorContext) bool {
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
	match := resolveOrderProductMatch(userText, nil, catalog, vctx)
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

func formatCatalogPicker(catalog []CatalogItem, max int) string {
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

func mergeShippingText(st *OrderState, userText string) {
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

func parseUnstructuredAddress(st *OrderState, lower, raw string) {
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

func (st OrderState) ShippingComplete() bool {
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

func (st OrderState) ProductComplete() bool {
	if st.HasMultiItems() {
		return st.StructuredLinesReady()
	}
	return strings.TrimSpace(st.ProductName) != "" || strings.TrimSpace(st.CatalogItemID) != ""
}

// catalogItemNeedsVariant — apparel/ukuran S-M-L. Makanan, gadget, kosmetik, dan
// SKU tanpa suffix ukuran tidak masuk ask_variant.
func catalogItemNeedsVariant(it *CatalogItem) bool {
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
		"jaket", "hoodie", "highwaist", "hotpants", "skinny",
	} {
		if strings.Contains(name, kw) {
			return true
		}
	}
	if strings.Contains(name, "celana dalam") || strings.Contains(name, "boxer anak") {
		return true
	}
	return false
}

func (st OrderState) VariantComplete() bool {
	it := &CatalogItem{Name: st.ProductName, ExternalCode: st.ExternalCode}
	if !catalogItemNeedsVariant(it) {
		return true
	}
	return st.Size != "" || st.Color != ""
}

func applyCatalogMatch(st *OrderState, it *CatalogItem) {
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

func catalogConfirmLine(st OrderState) string {
	summary := formatOrderSummary(st)
	if summary != "" {
		return summary
	}
	if st.ProductName == "" {
		return ""
	}
	it := &CatalogItem{
		Name:         st.ProductName,
		SellPrice:    st.UnitPrice,
		SellUnit:     st.SellUnit,
		ExternalCode: st.ExternalCode,
	}
	return strings.TrimSpace("Produk:\n" + st.ProductName + "\n\nHarga:\n" + formatCatalogPrice(it))
}

func missingOrderDataPrompt(st OrderState, tmpl orderFlowTemplates) string {
	st = normalizeOrderState(st)
	if st.HasMultiItems() {
		if !st.StructuredLinesReady() {
			return tmpl.AskVariant
		}
	} else {
		if !st.ProductComplete() {
			return tmpl.AskProduct
		}
		if !st.VariantComplete() {
			return tmpl.AskVariant
		}
		if st.Qty < 1 {
			return tmpl.AskQty
		}
	}
	if strings.TrimSpace(st.RecipientName) == "" || strings.TrimSpace(st.RecipientPhone) == "" {
		return tmpl.AskRecipient
	}
	if !st.ShippingComplete() {
		return tmpl.ClarifyAddress
	}
	return ""
}
