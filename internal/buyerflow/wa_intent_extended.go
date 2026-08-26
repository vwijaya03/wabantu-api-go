package buyerflow

import "strings"

// IsMinimumOrderQuestion — MOQ / minimal pembelian.
func IsMinimumOrderQuestion(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	signals := []string{
		"minimum order", "min order", "min pesan", "min pesan berapa", "min beli", "minimal order",
		"minimal pesan", "minimal beli", "berapa minimal", "minimum pembelian",
		"min pembelian", "order minimum", "pesan minimal",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	if strings.Contains(text, "bisa order") || strings.Contains(text, "bisa pesan") ||
		strings.Contains(text, "boleh order") || strings.Contains(text, "boleh pesan") {
		return true
	}
	return strings.Contains(text, "minimal") &&
		(strings.Contains(text, "?") || strings.Contains(text, "berapa") || strings.Contains(text, "bisa"))
}

// IsProductComparisonQuestion — bandingkan dua produk.
func IsProductComparisonQuestion(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	compare := strings.Contains(text, "beda") || strings.Contains(text, "banding") ||
		strings.Contains(text, " vs ") || strings.Contains(text, "versus") ||
		strings.Contains(text, " mana yang") || strings.Contains(text, "lebih bagus")
	hasProduct := strings.Contains(text, "boxer") || strings.Contains(text, "abon") ||
		strings.Contains(text, "produk") || strings.Contains(text, "jeans")
	return compare && (hasProduct || strings.Contains(text, "?"))
}

// IsRecommendationRequest — minta saran produk.
func IsRecommendationRequest(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if IsExplicitNewOrderStart(userText) || IsStructuredOrderList(userText) {
		return false
	}
	if hasPurchaseIntent(userText, nil) || mentionsOrderQty(userText) {
		return false
	}
	signals := []string{
		"rekomend", "rekomendas", "sarankan", "saranin", "saran dong",
		"recommend", "suggestion", "paling laris", "paling recommended",
		"rekomendasi best seller", "minta best seller", "yang best seller",
		"produk best seller", "ada best seller",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// IsComplaintLike — komplain / retur / produk rusak.
func IsComplaintLike(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	signals := []string{
		"komplain", "complaint", "kecewa", "mogok", "refund", "retur", "return",
		"rusak", "cacat", "salah kirim", "barang salah", "tidak sesuai", "zonk",
		"nipu", "penipuan", "laporin", "laporkan",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// IsHumanEscalationRequest — minta CS/manusia.
func IsHumanEscalationRequest(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if strings.Contains(text, "hubungi admin") || strings.Contains(text, "chat cs") ||
		strings.Contains(text, "customer service") || strings.Contains(text, "sambungkan") {
		return true
	}
	human := strings.Contains(text, "manusia") || strings.Contains(text, "operator") ||
		strings.Contains(text, "admin") || strings.Contains(text, " cs") || strings.HasPrefix(text, "cs ") ||
		strings.Contains(text, " cs ") || strings.HasSuffix(text, " cs") || strings.Contains(text, "sama cs")
	return human && (strings.Contains(text, "?") || strings.Contains(text, "mau") || strings.Contains(text, "hubung") ||
		strings.Contains(text, "chat") || strings.Contains(text, "sambung"))
}

// IsPaymentQuestion — pembayaran / transfer / COD.
func IsPaymentQuestion(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if IsOrderStatusInquiry(userText) {
		return false
	}
	signals := []string{
		"bayar", "pembayaran", "transfer", "trf", "tf ke", "cod", "qris",
		"rekening", "bank", "va ", "virtual account", "cara bayar", "payment",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// IsAbandonedCartSignal — user ninggalin checkout / nanti dulu.
func IsAbandonedCartSignal(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	signals := []string{
		"nanti dulu", "ntar dulu", "besok aja", "pikirin dulu", "tunggu dulu",
		"belum dulu", "skip dulu", "udah dulu", "dulu deh",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// IsAmbiguousPurchaseSignal — bisa konsultasi bisa checkout.
func IsAmbiguousPurchaseSignal(userText string) bool {
	if IsConsultingPurchaseQuestion(userText, nil) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	return strings.Contains(text, "mau beli") && strings.Contains(text, "?")
}
