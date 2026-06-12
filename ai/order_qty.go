package ai

import (
	"regexp"
	"strings"
)

var orderQtyPackRe = regexp.MustCompile(`(?i)(?:^|\s)(\d{1,4})\s*paket`)

// orderRevisionSignals — inti deteksi revisi qty (tanpa memanggil IsPricingUnitClarification).
func orderRevisionSignals(text, userText string) bool {
	if text == "" {
		return false
	}
	if strings.Contains(text, "bukan") &&
		(strings.Contains(text, "paket") || strings.Contains(text, "pcs") || strings.Contains(text, "biji")) {
		return true
	}
	hasRevisionVerb := strings.Contains(text, "ubah") || strings.Contains(text, "rubah") ||
		strings.Contains(text, "revisi") || strings.Contains(text, "ganti") ||
		strings.Contains(text, "gantiin")
	if hasRevisionVerb {
		if _, ok := parseOrderQty(userText); ok {
			return true
		}
		if strings.Contains(text, "paket") || strings.Contains(text, "jadi") {
			return true
		}
	}
	return false
}

// IsOrderRevisionMessage — koreksi/revisi jumlah pesanan (bukan pesanan baru / tanya harga).
func IsOrderRevisionMessage(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	return orderRevisionSignals(text, userText)
}

// tryApplyQtyRevision updates qty when customer revises count mid-checkout.
func tryApplyQtyRevision(st *orderState, userText string) bool {
	if st == nil {
		return false
	}
	q, ok := parseOrderQty(userText)
	if !ok || q < 1 {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	isRevision := IsOrderRevisionMessage(userText) ||
		strings.Contains(text, "ubah") || strings.Contains(text, "revisi") ||
		strings.Contains(text, "ganti") || (st.Qty > 0 && q != st.Qty)
	if !isRevision {
		return false
	}
	st.Qty = q
	return true
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
