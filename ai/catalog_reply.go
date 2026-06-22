package ai

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	catalogPackInNameRe    = regexp.MustCompile(`(?i)\[\s*(\d+)\s*pcs\s*\]`)
	catalogLeadingPackRe   = regexp.MustCompile(`(?i)^(\d+)\s*pcs\b`)
)

const catalogEmptyMarker = "[Katalog WABantu: kosong]"

// IsCatalogBrowsingIntent — pelanggan masih browsing katalog (bukan checkout).
func IsCatalogBrowsingIntent(userText string) bool {
	if IsCatalogListQuestion(userText) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	phrases := []string{
		"tanya produk", "tanya-tanya produk", "nanya produk",
		"produk di toko", "produk yang dijual", "yang dijual di toko",
		"tunjukkan produk", "tunjukan produk", "lihat produk", "liat katalog",
		"mau liat katalog", "mau lihat produk",
		"beberapa produk", "produk apa", "produknya apa", "jual apa", "jualan apa",
		"tersedia jualan", "tersedia produk", "tersedia barang",
		"di toko ini", "di toko ada", "toko ini jual",
	}
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// IsCatalogListQuestion — pelanggan minta daftar produk/katalog.
func IsCatalogListQuestion(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	phrases := []string{
		"daftar produk", "list produk", "list barang", "daftar barang",
		"list sku", "daftar sku", "list kode", "minta sku", "lihat sku",
		"lihat katalog", "tunjukkan katalog", "tampilkan katalog", "show katalog",
		"produk apa saja", "barang apa saja", "apa saja produk", "apa saja barang",
		"jualan apa saja", "jualan apa aja", "jual apa saja", "jual apa aja",
		"menu apa saja", "menu apa aja", "show catalog", "show katalog",
		"katalog apa", "koleksi apa", "jenis produk", "macam produk", "macam barang",
		"minta list", "kasih list", "berikan list", "kirim list", "list dong",
		"listkan", "listkan semua", "semua jualan", "semua jualan kamu",
		"ada produk apa", "tersedia apa", "tersedia jualan", "menjual apa",
	}
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	words := strings.Fields(text)
	if len(words) <= 5 {
		hasList := strings.Contains(text, "list") || strings.Contains(text, "daftar")
		hasCatalog := strings.Contains(text, "katalog") || strings.Contains(text, "catalog")
		hasProduct := strings.Contains(text, "produk") || strings.Contains(text, "barang")
		if (hasList || hasCatalog) && (hasProduct || hasCatalog || len(words) <= 3) {
			return true
		}
	}
	return false
}

// isGeneralStoreCatalogQuestion — tanya isi toko secara umum (bukan satu SKU).
func isGeneralStoreCatalogQuestion(userText string) bool {
	if IsCatalogListQuestion(userText) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	signals := []string{
		"apa saja", "apa aja", "apa2",
		"jualan apa", "jual apa", "menjual apa",
		"kamu jualan", "kamu jual", "lu jualan", "lu jual",
		"tersedia apa", "ada apa", "punya apa",
		"semua produk", "semua barang", "macam-macam",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	if strings.Contains(text, "di toko") &&
		(strings.Contains(text, "jualan") || strings.Contains(text, "produk") || strings.Contains(text, "barang")) {
		return true
	}
	return false
}

// IsProductSellInquiry — "jual abon sapi?" (produk tertentu dijual atau tidak).
func IsProductSellInquiry(userText string, catalog []dbCatalogItem) bool {
	if HasPurchaseIntent(userText) || IsConsultingPurchaseQuestion(userText, catalog) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || !IsQuestionLike(userText) {
		return false
	}
	if isGeneralStoreCatalogQuestion(userText) {
		return false
	}
	hasSell := strings.Contains(text, "jual") || strings.Contains(text, "jualan") || strings.Contains(text, "menjual")
	if !hasSell {
		return false
	}
	return matchCatalogItem(userText, catalog) != nil
}

// IsCatalogProductInquiry — tanya harga/stok produk tertentu (bukan checkout).
func IsCatalogProductInquiry(userText string) bool {
	if HasPurchaseIntent(userText) || IsConsultingPurchaseQuestion(userText, nil) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if isGeneralStoreCatalogQuestion(userText) {
		return false
	}
	if isGenericVisualProductInquiry(userText, nil) {
		return false
	}
	hints := []string{"harga", "harg", "stok", "stock", "ready", "berapa", "brp", "rp ", "rp.", "price", "masih ada"}
	// "tersedia" hanya inquiry produk jika tidak tanya umum (mis. "stok jeans ready?").
	if strings.Contains(text, "tersedia") && !strings.Contains(text, "apa") {
		hints = append(hints, "tersedia")
	}
	for _, h := range hints {
		if strings.Contains(text, h) {
			return true
		}
	}
	return false
}

// IsPricingUnitClarification — tanya satuan harga (per pcs/paket/piece), sering follow-up "itu harga...".
func IsPricingUnitClarification(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	signals := []string{
		"per piece", "per paket", "per pcs", "per pc", "per potong", "per biji",
		"per unit", "satu pcs", "satu paket", "satu biji", "harga per",
		"hitung per", "bayar per",
		"paket isi", "isi berapa", "isi nya berapa", "isinya berapa",
		"satu paket isi",
	}
	if orderRevisionSignals(strings.ToLower(strings.TrimSpace(userText)), userText) {
		return false
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	if strings.Contains(text, "bingung") &&
		(strings.Contains(text, "harga") || strings.Contains(text, "pcs") || strings.Contains(text, "paket") || strings.Contains(text, "piece")) {
		return true
	}
	if (strings.Contains(text, "itu") || strings.Contains(text, "ini")) &&
		strings.Contains(text, "harga") &&
		(strings.Contains(text, "pcs") || strings.Contains(text, "paket") || strings.Contains(text, "piece")) {
		return true
	}
	return false
}

func isAvailabilityQuestion(text string) bool {
	signals := []string{
		"masih ada", "ada ga", "ada gak", "ada nggak", "ada tidak",
		"ready", "stok", "stock", "tersedia", "habis", "kosong",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// isGenericVisualProductInquiry — caption/foto tanpa nama produk jelas (vision WA belum dipakai).
func isGenericVisualProductInquiry(userText string, catalog []dbCatalogItem) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || !isAvailabilityQuestion(text) {
		return false
	}
	visualSignals := []string{
		"produk ini", "barang ini", "item ini",
		"di foto", "di gambar", "foto ini", "gambar ini",
		"foto tadi", "gambar tadi", "yang di foto", "yang di gambar",
	}
	generic := false
	for _, s := range visualSignals {
		if strings.Contains(text, s) {
			generic = true
			break
		}
	}
	if !generic && (strings.Contains(text, "produk ini") || strings.Contains(text, "barang ini")) {
		generic = true
	}
	if !generic {
		return false
	}
	if match := matchCatalogItem(userText, catalog); match != nil {
		nameLower := strings.ToLower(match.Name)
		userToks := tokenize(text)
		if overlapScore(userToks, tokenize(nameLower)) >= 0.2 {
			return false
		}
		for _, tok := range userToks {
			if len(tok) >= 4 && strings.Contains(nameLower, tok) {
				return false
			}
		}
	}
	return true
}

func isCatalogContextualReference(userText string) bool {
	if isGenericVisualProductInquiry(userText, nil) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	prefixes := []string{"itu ", "yang tadi", "yang barusan", "produk itu", "harga itu", "yang ini"}
	for _, p := range prefixes {
		if strings.HasPrefix(text, p) || strings.Contains(text, " "+strings.TrimSpace(p)) {
			return true
		}
	}
	// "ini harga ..." follow-up — bukan "produk ini masih ada"
	if strings.Contains(text, " ini ") && strings.Contains(text, "harga") {
		return true
	}
	return false
}

func isCatalogListOutbound(body string) bool {
	text := strings.ToLower(body)
	if strings.Contains(text, "produk pilihan") || strings.Contains(text, "ini katalog") ||
		strings.Contains(text, "belum ketemu") || strings.Contains(text, "belum kami temukan") ||
		strings.Contains(text, "contoh produk") {
		return true
	}
	return false
}

func countCatalogMatchesInText(body string, catalog []dbCatalogItem) int {
	n := 0
	seen := make(map[string]struct{})
	for _, it := range catalog {
		if it.ID == "" {
			continue
		}
		if matchCatalogItem(body, []dbCatalogItem{it}) != nil {
			if _, ok := seen[it.ID]; !ok {
				seen[it.ID] = struct{}{}
				n++
			}
		}
	}
	return n
}

func matchCatalogFromFocusedHistory(history []dbMessage, catalog []dbCatalogItem) *dbCatalogItem {
	if len(history) == 0 || len(catalog) == 0 {
		return nil
	}
	seen := 0
	for i := len(history) - 1; i >= 0 && seen < 8; i-- {
		if history[i].Direction != "out" {
			continue
		}
		seen++
		body := history[i].Body
		if isCatalogListOutbound(body) || countCatalogMatchesInText(body, catalog) > 1 {
			continue
		}
		if match := matchCatalogItem(body, catalog); match != nil {
			return match
		}
	}
	return nil
}

func isStockOrAvailabilityFollowUp(userText string, catalog []dbCatalogItem) bool {
	if messageNamesCatalogProduct(userText, catalog) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	signals := []string{
		"stoknya", "stok nya", "stoknya ada", "stok ada",
		"ready ga", "ready gak", "ready nggak",
		"masih ready", "masih ada", "ready ?", "ready?",
		"ada ga", "ada gak", "ada nggak",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	words := strings.Fields(strings.NewReplacer("?", "", "!", "").Replace(text))
	if len(words) <= 3 && (text == "ada" || strings.HasPrefix(text, "ada ")) &&
		(strings.Contains(text, "?") || IsQuestionLike(userText)) {
		return true
	}
	if (strings.Contains(text, "stok") || strings.Contains(text, "ready")) &&
		(strings.Contains(text, "?") || IsQuestionLike(userText)) {
		if len(words) <= 4 {
			return true
		}
	}
	return false
}

func matchCatalogFromRecentOutbound(history []dbMessage, catalog []dbCatalogItem) *dbCatalogItem {
	if len(history) == 0 || len(catalog) == 0 {
		return nil
	}
	seen := 0
	for i := len(history) - 1; i >= 0 && seen < 4; i-- {
		if history[i].Direction != "out" {
			continue
		}
		seen++
		if match := matchCatalogItem(history[i].Body, catalog); match != nil {
			return match
		}
	}
	return nil
}

func resolveCatalogMatch(userText string, history []dbMessage, catalog []dbCatalogItem) *dbCatalogItem {
	if isGenericVisualProductInquiry(userText, catalog) {
		return nil
	}
	if match := matchCatalogItem(userText, catalog); match != nil {
		if !IsConsultingPurchaseQuestion(userText, catalog) || catalogProductExplicitlyNamed(userText, match) {
			return match
		}
	}
	if isCatalogContextualReference(userText) || IsPricingUnitClarification(userText) ||
		IsConsultingPurchaseQuestion(userText, catalog) || isStockOrAvailabilityFollowUp(userText, catalog) {
		if match := matchCatalogFromFocusedHistory(history, catalog); match != nil {
			return match
		}
		return matchCatalogFromRecentOutbound(history, catalog)
	}
	return nil
}

func extractPackCountFromName(name string) int {
	if n := bracketPackCount(name); n > 1 {
		return n
	}
	if m := catalogLeadingPackRe.FindStringSubmatch(name); len(m) > 1 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 1 {
			return n
		}
	}
	return 0
}

// bracketPackCount — judul [N PCS] di Omah Apparel: sell_price = harga 1 paket (isi N pcs), bukan per pcs.
func bracketPackCount(name string) int {
	if m := catalogPackInNameRe.FindStringSubmatch(name); len(m) > 1 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 1 {
			return n
		}
	}
	return 0
}

type catalogPriceInfo struct {
	packCount       int
	isPackListing   bool
	listPrice       float64
	unitLabel       string
	perPiecePrice   float64
}

func parseCatalogPriceInfo(it *dbCatalogItem) catalogPriceInfo {
	info := catalogPriceInfo{unitLabel: "pcs"}
	if it == nil || it.SellPrice <= 0 {
		return info
	}
	unit := strings.TrimSpace(it.SellUnit)
	if unit == "" {
		unit = "pcs"
	}
	info.listPrice = it.SellPrice
	info.packCount = bracketPackCount(it.Name)
	if info.packCount > 1 {
		info.isPackListing = true
		info.unitLabel = "paket"
		info.perPiecePrice = it.SellPrice / float64(info.packCount)
	} else {
		info.unitLabel = unit
	}
	return info
}

func buildPricingClarificationReply(formal bool, it *dbCatalogItem) string {
	if it == nil {
		return ""
	}
	price := parseCatalogPriceInfo(it)
	name := shortDisplayName(it.Name)
	priceLine := formatCatalogPrice(it)

	var lines []string
	if formal {
		lines = append(lines, fmt.Sprintf("Untuk %s:", name))
	} else {
		lines = append(lines, fmt.Sprintf("Kak, untuk %s:", name))
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Harga di katalog: %s.", priceLine))
	if price.isPackListing {
		if formal {
			lines = append(lines, fmt.Sprintf("1 paket berisi %d pcs.", price.packCount))
			lines = append(lines, fmt.Sprintf("Per pcs ≈ %s (%s ÷ %d).", formatMoney(price.perPiecePrice), formatMoney(price.listPrice), price.packCount))
		} else {
			lines = append(lines, fmt.Sprintf("Judul [%d PCS] = 1 paket isi %d pcs ya kak.", price.packCount, price.packCount))
			lines = append(lines, fmt.Sprintf("Harga 1 biji ≈ %s (%s ÷ %d).", formatMoney(price.perPiecePrice), formatMoney(price.listPrice), price.packCount))
		}
	}
	cta := "Mau pesan per pcs atau per paket? Sebut jumlahnya ya kak."
	if formal {
		cta = "Silakan sebutkan jumlah pesanan (per pcs atau per paket)."
	}
	if price.isPackListing {
		cta = "Kalau mau pesan, sebut berapa paket ya kak."
		if formal {
			cta = "Jika ingin memesan, sebutkan jumlah paket."
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n") + "\n\n" + cta)
}

func buildRetailPolicyReply(formal bool, it *dbCatalogItem) string {
	if it == nil {
		return ""
	}
	price := parseCatalogPriceInfo(it)
	name := shortDisplayName(it.Name)
	priceLine := formatCatalogPrice(it)

	var lines []string
	if price.isPackListing {
		if formal {
			lines = append(lines, fmt.Sprintf("Untuk %s:", name))
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("Dijual per paket isi %d pcs (belum eceran 1 biji terpisah).", price.packCount))
			lines = append(lines, fmt.Sprintf("Harga paket: %s.", priceLine))
			lines = append(lines, fmt.Sprintf("Per pcs ≈ %s.", formatMoney(price.perPiecePrice)))
		} else {
			lines = append(lines, fmt.Sprintf("Kak, %s dijual per paket isi %d pcs ya — belum eceran 1 biji.", name, price.packCount))
			lines = append(lines, fmt.Sprintf("Harga 1 paket: %s (~%s/biji).", priceLine, formatMoney(price.perPiecePrice)))
		}
	} else {
		if formal {
			lines = append(lines, fmt.Sprintf("Bisa kak, %s dijual per %s.", name, price.unitLabel))
			lines = append(lines, fmt.Sprintf("Harga: %s.", priceLine))
		} else {
			lines = append(lines, fmt.Sprintf("Bisa kak, %s bisa beli per %s.", name, price.unitLabel))
			lines = append(lines, fmt.Sprintf("Harganya %s.", priceLine))
		}
	}
	cta := "Kalau sudah mau pesan, bilang aja ya kak — nanti saya bantu data pengirimannya."
	if formal {
		cta = "Jika ingin memesan, silakan beri tahu — kami bantu proses pengiriman."
	}
	return strings.TrimSpace(strings.Join(lines, "\n") + "\n\n" + cta)
}

// formatStockLabel renders available stock; whole numbers stay integer-clean.
func formatStockLabel(qty float64) string {
	if qty <= 0 {
		return "habis"
	}
	if qty == float64(int64(qty)) {
		return fmt.Sprintf("%d", int64(qty))
	}
	return fmt.Sprintf("%g", qty)
}

// replyFromBusinessCatalog answers from business_catalog_item without LLM/KB hijack.
func replyFromBusinessCatalog(
	userText string,
	profile *dbBusinessProfile,
	catalog []dbCatalogItem,
	history []dbMessage,
) (reply string, handled bool) {
	if profile == nil {
		return "", false
	}
	formal := strOrEmpty(profile.Tone) == "formal"
	bizName := strings.TrimSpace(profile.BusinessName)

	if isGenericVisualProductInquiry(userText, catalog) {
		return buildGenericVisualProductInquiryReply(formal, bizName, catalog, profile), true
	}

	if IsExplicitNewOrderStart(userText) || IsStructuredOrderList(userText) {
		return "", false
	}

	if IsCatalogBrowsingIntent(userText) || isGeneralStoreCatalogQuestion(userText) ||
		IsRecommendationRequest(userText) {
		return buildCatalogListReply(formal, bizName, catalog, profile), true
	}

	// Checkout eksplisit → order flow state machine, bukan balasan katalog statis.
	if hasPurchaseIntent(userText, catalog) {
		return "", false
	}

	// Revisi pesanan (qty/paket) → order flow, bukan katalog.
	if IsOrderRevisionMessage(userText) {
		return "", false
	}

	if isHistoryBackedPurchaseIntent(userText, history, catalog) {
		return "", false
	}

	if IsProductSellInquiry(userText, catalog) {
		if match := resolveCatalogMatch(userText, history, catalog); match != nil {
			return buildCatalogItemReply(formal, match, 0), true
		}
	}

	if IsConsultingPurchaseQuestion(userText, catalog) {
		if match := resolveCatalogMatch(userText, history, catalog); match != nil {
			text := strings.ToLower(strings.TrimSpace(userText))
			if isAvailabilityQuestion(text) || isStockOrAvailabilityFollowUp(userText, catalog) {
				qty, _ := parseOrderQty(userText)
				return buildCatalogItemReply(formal, match, qty), true
			}
			return buildRetailPolicyReply(formal, match), true
		}
	}

	if IsPricingUnitClarification(userText) || isCatalogContextualReference(userText) {
		if match := resolveCatalogMatch(userText, history, catalog); match != nil {
			return buildPricingClarificationReply(formal, match), true
		}
		return "", false
	}

	if IsCatalogProductInquiry(userText) {
		if len(catalog) == 0 {
			return buildCatalogEmptyReply(formal, bizName, profile), true
		}
		if match := resolveCatalogMatch(userText, history, catalog); match != nil {
			if IsPricingUnitClarification(userText) {
				return buildPricingClarificationReply(formal, match), true
			}
			qty, _ := parseOrderQty(userText)
			return buildCatalogItemReply(formal, match, qty), true
		}
		if isCatalogContextualReference(userText) || IsPricingUnitClarification(userText) {
			return "", false
		}
		return buildCatalogNotFoundReply(formal, bizName, catalog, profile), true
	}

	// Nama produk disebut tanpa kata harga — tetap coba cocokkan jika jelas ke katalog.
	if len(catalog) > 0 && !IsQuestionLike(userText) {
		if match := matchCatalogItem(userText, catalog); match != nil {
			if strings.Contains(strings.ToLower(userText), strings.ToLower(match.Name)) ||
				overlapScore(tokenize(userText), tokenize(match.Name)) >= 0.2 {
				qty, _ := parseOrderQty(userText)
				return buildCatalogItemReply(formal, match, qty), true
			}
		}
	}
	return "", false
}

func buildGenericVisualProductInquiryReply(formal bool, bizName string, catalog []dbCatalogItem, profile *dbBusinessProfile) string {
	var head string
	if formal {
		head = "Mohon maaf, saat ini kami belum bisa mengenali produk langsung dari foto di chat WhatsApp.\n\nSilakan sebut nama produknya (misalnya dari label kemasan), nanti kami cek stok dan harganya."
	} else {
		head = "Maaf kak, saya belum bisa baca produk langsung dari foto di chat ini 🙏\n\nTolong sebut nama produknya ya kak (misalnya Abon Sapi), nanti saya cek stok & harganya."
	}
	if len(catalog) == 0 {
		return strings.TrimSpace(head + catalogExternalFooter(profile, true))
	}
	body := "\n\nContoh produk di katalog kami:\n\n" + formatCatalogListBody(catalog, 5)
	footer := catalogExternalFooter(profile, false)
	return strings.TrimSpace(head + body + footer)
}

func buildCatalogListReply(formal bool, bizName string, catalog []dbCatalogItem, profile *dbBusinessProfile) string {
	if len(catalog) == 0 {
		return buildCatalogEmptyReply(formal, bizName, profile)
	}
	var intro string
	if formal {
		intro = fmt.Sprintf("Berikut katalog %s kami:\n\n", bizName)
	} else {
		intro = fmt.Sprintf("Ini katalog %s ya kak:\n\n", bizName)
	}
	body := formatCatalogListBody(catalog, 8)
	footer := catalogExternalFooter(profile, false)
	return strings.TrimSpace(intro + body + footer)
}

func buildCatalogEmptyReply(formal bool, bizName string, profile *dbBusinessProfile) string {
	marker := catalogEmptyMarker
	if formal {
		base := fmt.Sprintf("%s\n\nKak, katalog produk di sistem WABantu untuk %s saat ini belum berisi item aktif (belum ada data di business_catalog_item). Tim kami bisa bantu info produk secara manual.",
			marker, bizName)
		return strings.TrimSpace(base + catalogExternalFooter(profile, true))
	}
	base := fmt.Sprintf("%s\n\nKak, katalog WABantu untuk %s masih kosong (belum ada produk yang diinput). Chat ini dulu ya, nanti tim CS bantu.",
		marker, bizName)
	return strings.TrimSpace(base + catalogExternalFooter(profile, true))
}

func buildCatalogNotFoundReply(formal bool, bizName string, catalog []dbCatalogItem, profile *dbBusinessProfile) string {
	var head string
	if formal {
		head = fmt.Sprintf("Maaf kak, produk yang Kakak sebut belum kami temukan di katalog WABantu %s.\n\nBerikut produk yang tersedia:\n\n", bizName)
	} else {
		head = fmt.Sprintf("Maaf kak, produknya belum ketemu di katalog WABantu %s.\n\nIni yang ada sekarang:\n\n", bizName)
	}
	body := formatCatalogListBody(catalog, 6)
	footer := catalogExternalFooter(profile, false)
	return strings.TrimSpace(head + body + footer)
}

func buildCatalogItemReply(formal bool, it *dbCatalogItem, qty int) string {
	if it == nil {
		return ""
	}
	size := extractSizeFromProductName(it.Name)
	var lines []string
	lines = append(lines, "Produk:")
	lines = append(lines, it.Name)
	lines = append(lines, "")
	lines = append(lines, "Harga:")
	lines = append(lines, formatCatalogPrice(it))
	if it.StockTracked {
		lines = append(lines, "")
		if len(it.StockByWarehouse) > 0 {
			lines = append(lines, formatStockBreakdownBlock(it.StockByWarehouse))
		} else {
			lines = append(lines, "Stok tersedia: "+formatStockLabel(it.StockAvailable))
		}
	}
	if size != "" && catalogItemNeedsVariant(it) {
		lines = append(lines, "")
		lines = append(lines, "Ukuran: "+size)
	}
	if qty > 0 && it.SellPrice > 0 {
		price := parseCatalogPriceInfo(it)
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("Qty: %d", qty))
		lines = append(lines, "")
		lines = append(lines, "Subtotal:")
		lines = append(lines, formatMoney(float64(qty)*price.listPrice))
	}
	cta := "Kalau mau lanjut order, sebut jumlah + data pengiriman ya kak."
	if formal {
		cta = "Silakan sebutkan jumlah dan data pengiriman jika ingin memesan."
	}
	return strings.TrimSpace(strings.Join(lines, "\n") + "\n\n" + cta)
}

func formatCatalogListBody(catalog []dbCatalogItem, maxFeatured int) string {
	if maxFeatured < 1 || maxFeatured > 8 {
		maxFeatured = 5
	}
	grouped := groupCatalogByCategory(catalog)
	cats := extractCatalogCategories(catalog)
	if len(cats) == 0 {
		for c := range grouped {
			cats = append(cats, c)
		}
		sort.Strings(cats)
	}

	var parts []string
	parts = append(parts, "🔥 Produk Pilihan\n")

	shown := 0
	for _, cat := range cats {
		if shown >= maxFeatured {
			break
		}
		items := grouped[cat]
		if len(items) == 0 {
			continue
		}
		parts = append(parts, categoryEmoji(cat)+" "+cat)
		for _, it := range items {
			if shown >= maxFeatured {
				break
			}
			parts = append(parts, fmt.Sprintf("• %s\n%s", shortDisplayName(it.Name), formatCatalogPrice(&it)))
			shown++
		}
	}
	if shown == 0 {
		featured := pickFeaturedCatalogItems(catalog, maxFeatured)
		parts = append(parts, formatFeaturedProductsBody(featured))
		shown = len(featured)
	}
	if len(catalog) > shown {
		parts = append(parts, fmt.Sprintf("\nAda produk lain di katalog. Sebut nama produk yang kakak mau ya."))
	}
	if len(cats) > 1 {
		parts = append(parts, "\nMau lihat kategori tertentu? Sebut saja ya kak.")
	}
	return strings.Join(parts, "\n")
}

func catalogExternalFooter(profile *dbBusinessProfile, catalogEmpty bool) string {
	url := strings.TrimSpace(strOrEmpty(profile.CatalogURL))
	if url == "" {
		return ""
	}
	if catalogEmpty {
		return "\n\nInfo tambahan (di luar katalog WABantu): " + url
	}
	return ""
}

// BuildCatalogContext injects DB catalog into LLM system context.
func BuildCatalogContext(catalog []dbCatalogItem) string {
	if len(catalog) == 0 {
		return catalogEmptyMarker + "\nKatalog resmi (database business_catalog_item): belum ada produk aktif. Jangan mengarang nama/harga produk. Jangan mengarahkan Instagram/website sebagai daftar utama — jelaskan katalog WABantu kosong."
	}
	var lines []string
	lines = append(lines, "Katalog resmi (database business_catalog_item — sumber utama produk & harga):")
	for i, it := range catalog {
		if i >= 30 {
			lines = append(lines, fmt.Sprintf("…+%d produk lainnya", len(catalog)-30))
			break
		}
		price := "harga belum di-set"
		if it.SellPrice > 0 {
			price = formatCatalogPrice(&it)
			if info := parseCatalogPriceInfo(&it); info.isPackListing {
				price += fmt.Sprintf(" (~%s/pcs)", formatMoney(info.perPiecePrice))
			}
		}
		lines = append(lines, fmt.Sprintf("- %s | %s | kode internal: %s", it.Name, price, it.ExternalCode))
	}
	lines = append(lines, "Aturan: jawab produk/harga hanya dari daftar di atas. Jangan tampilkan kode internal/SKU ke pelanggan. Jangan dump daftar panjang — gunakan kategori + produk unggulan. Jangan mengarang produk di luar daftar.")
	return strings.Join(lines, "\n")
}
