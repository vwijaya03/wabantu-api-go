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
			return buildCatalogItemReply(formal, match), true
		}
		return buildCatalogNotFoundReply(formal, bizName, catalog, profile), true
	}

	// Nama produk disebut tanpa kata harga — tetap coba cocokkan jika jelas ke katalog.
	if len(catalog) > 0 && !IsQuestionLike(userText) {
		if match := matchCatalogItem(userText, catalog); match != nil {
			if strings.Contains(strings.ToLower(userText), strings.ToLower(match.Name)) ||
				overlapScore(tokenize(userText), tokenize(match.Name)) >= 0.2 {
				return buildCatalogItemReply(formal, match), true
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
		intro = fmt.Sprintf("Berikut daftar produk aktif %s dari katalog WABantu kami:\n\n", bizName)
	} else {
		intro = fmt.Sprintf("Ini daftar produk aktif %s dari katalog WABantu ya kak:\n\n", bizName)
	}
	body := formatCatalogListBody(catalog, 25)
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
	body := formatCatalogListBody(catalog, 20)
	footer := catalogExternalFooter(profile, false)
	return strings.TrimSpace(head + body + footer)
}

func buildCatalogItemReply(formal bool, it *dbCatalogItem) string {
	if it == nil {
		return ""
	}
	unit := it.SellUnit
	if unit == "" {
		unit = "pcs"
	}
	priceLine := "harga belum di-set"
	if it.SellPrice > 0 {
		priceLine = fmt.Sprintf("Rp%.0f/%s", it.SellPrice, unit)
	}
	if formal {
		return fmt.Sprintf("Produk: %s (kode %s)\nHarga: %s\n\nSilakan sebutkan ukuran/warna dan jumlah jika ingin memesan.",
			it.Name, it.ExternalCode, priceLine)
	}
	return fmt.Sprintf("Produk: %s (kode %s)\nHarga: %s\n\nKalau mau order, sebut ukuran/warna + jumlah ya kak.",
		it.Name, it.ExternalCode, priceLine)
}

func formatCatalogListBody(catalog []dbCatalogItem, max int) string {
	if max < 1 || max > 40 {
		max = 25
	}
	var lines []string
	for i := 0; i < len(catalog) && i < max; i++ {
		lines = append(lines, formatCatalogLine(&catalog[i], i+1))
	}
	if len(catalog) > max {
		lines = append(lines, fmt.Sprintf("…dan %d produk lainnya.", len(catalog)-max))
	}
	return strings.Join(lines, "\n")
}

func formatCatalogLine(it *dbCatalogItem, num int) string {
	if it == nil {
		return ""
	}
	price := ""
	if it.SellPrice > 0 {
		unit := it.SellUnit
		if unit == "" {
			unit = "pcs"
		}
		price = fmt.Sprintf(" — Rp%.0f/%s", it.SellPrice, unit)
	}
	return fmt.Sprintf("%d. %s%s [%s]", num, it.Name, price, it.ExternalCode)
}

func catalogExternalFooter(profile *dbBusinessProfile, catalogEmpty bool) string {
	url := strings.TrimSpace(strOrEmpty(profile.CatalogURL))
	if url == "" {
		return ""
	}
	if catalogEmpty {
		return "\n\nInfo tambahan (di luar katalog WABantu): " + url
	}
	return "\n\nInfo lengkap tambahan: " + url
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
		lines = append(lines, fmt.Sprintf("- %s [%s] %s", it.Name, it.ExternalCode, price))
	}
	lines = append(lines, "Aturan: jawab produk/harga hanya dari daftar di atas. Jangan arahkan Instagram/website sebagai sumber utama jika daftar ini terisi. Jangan mengarang produk di luar daftar.")
	return strings.Join(lines, "\n")
}
