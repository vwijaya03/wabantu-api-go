package ai

import (
	"fmt"
	"strings"
)

const catalogEmptyMarker = "[Katalog WABantu: kosong]"

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
		"katalog apa", "koleksi apa", "jenis produk", "macam produk", "macam barang",
		"minta list", "kasih list", "berikan list", "kirim list", "list dong",
		"ada produk apa", "jual apa aja", "jual apa saja",
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

// IsCatalogProductInquiry — tanya harga/stok produk tertentu (bukan checkout).
func IsCatalogProductInquiry(userText string) bool {
	if HasPurchaseIntent(userText) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if IsCatalogListQuestion(userText) {
		return false
	}
	hints := []string{"harga", "stok", "stock", "ready", "tersedia", "berapa", "rp ", "rp."}
	for _, h := range hints {
		if strings.Contains(text, h) {
			return true
		}
	}
	return false
}

// replyFromBusinessCatalog answers from business_catalog_item without LLM/KB hijack.
func replyFromBusinessCatalog(
	userText string,
	profile *dbBusinessProfile,
	catalog []dbCatalogItem,
) (reply string, handled bool) {
	if profile == nil {
		return "", false
	}
	formal := strOrEmpty(profile.Tone) == "formal"
	bizName := strings.TrimSpace(profile.BusinessName)

	if IsCatalogListQuestion(userText) {
		return buildCatalogListReply(formal, bizName, catalog, profile), true
	}

	if IsCatalogProductInquiry(userText) {
		if len(catalog) == 0 {
			return buildCatalogEmptyReply(formal, bizName, profile), true
		}
		if match := matchCatalogItem(userText, catalog); match != nil {
			qty, _ := parseOrderQty(userText)
			return buildCatalogItemReply(formal, match, qty), true
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
	if size != "" {
		lines = append(lines, "")
		lines = append(lines, "Ukuran: "+size)
	}
	if qty > 0 && it.SellPrice > 0 {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("Qty: %d", qty))
		lines = append(lines, "")
		lines = append(lines, "Subtotal:")
		lines = append(lines, formatMoney(float64(qty)*it.SellPrice))
	}
	cta := "Kalau mau lanjut order, sebut jumlah + data pengiriman ya kak."
	if formal {
		cta = "Silakan sebutkan jumlah dan data pengiriman jika ingin memesan."
	}
	return strings.TrimSpace(strings.Join(lines, "\n") + "\n\n" + cta)
}

func formatCatalogListBody(catalog []dbCatalogItem, maxFeatured int) string {
	if maxFeatured < 1 || maxFeatured > 12 {
		maxFeatured = 8
	}
	var parts []string
	if categories := extractCatalogCategories(catalog); len(categories) > 0 {
		parts = append(parts, "📂 Kategori\n"+formatCategoryList(categories, 8))
	}
	featured := pickFeaturedCatalogItems(catalog, maxFeatured)
	if len(featured) > 0 {
		parts = append(parts, "🔥 Produk Terlaris\n\n"+formatFeaturedProductsBody(featured))
	}
	if len(catalog) > len(featured) {
		parts = append(parts, fmt.Sprintf("Ada %d varian lain di katalog. Sebut nama produk yang kakak mau ya.", len(catalog)))
	}
	return strings.Join(parts, "\n\n")
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
		unit := it.SellUnit
		if unit == "" {
			unit = "pcs"
		}
		price := "harga belum di-set"
		if it.SellPrice > 0 {
			price = fmt.Sprintf("Rp%.0f/%s", it.SellPrice, unit)
		}
		lines = append(lines, fmt.Sprintf("- %s | %s | kode internal: %s", it.Name, price, it.ExternalCode))
	}
	lines = append(lines, "Aturan: jawab produk/harga hanya dari daftar di atas. Jangan tampilkan kode internal/SKU ke pelanggan. Jangan dump daftar panjang — gunakan kategori + produk unggulan. Jangan mengarang produk di luar daftar.")
	return strings.Join(lines, "\n")
}
