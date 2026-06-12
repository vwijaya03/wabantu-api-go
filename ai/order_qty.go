package ai

import (
	"regexp"
	"strings"
)

var orderQtyPackRe = regexp.MustCompile(`(?i)(?:^|\s)(\d{1,4})\s*paket`)

// IsOrderRevisionMessage — koreksi/revisi jumlah pesanan (bukan tanya harga).
func IsOrderRevisionMessage(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if q, ok := parseOrderQty(userText); ok && q > 0 {
		if strings.Contains(text, "order") || strings.Contains(text, "pesan") ||
			strings.Contains(text, "beli") || strings.Contains(text, "paket") ||
			strings.Contains(text, "bukan") || strings.Contains(text, "ganti") {
			return true
		}
	}
	if strings.Contains(text, "bukan") &&
		(strings.Contains(text, "paket") || strings.Contains(text, "pcs") || strings.Contains(text, "biji")) {
		return true
	}
	return false
}

// IsCasualPraiseLike — pujian singkat ke bot, tetap in-scope.
func IsCasualPraiseLike(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || len(strings.Fields(text)) > 10 {
		return false
	}
	signals := []string{
		"pinter", "pandai", "mantap", "keren", "bagus", "top", "jos", "joss",
		"oke udah", "nah oke", "sip udah", "lu udah", "kamu udah",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

func casualPraiseReply(formal bool) string {
	if formal {
		return "Terima kasih kak 😊 Ada yang masih ingin ditanyakan atau mau lanjut pesan?"
	}
	return "Makasih kak 😊 Ada yang masih mau ditanyain atau mau lanjut pesan?"
}

func inferVariantFromProductName(st *orderState) {
	if st == nil || strings.TrimSpace(st.Size) != "" {
		return
	}
	if sz := extractSizeFromProductName(st.ProductName); sz != "" {
		st.Size = sz
	}
}
