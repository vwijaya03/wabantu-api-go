package buyerflow

import (
	"strings"
)

// IsCartRecapOrComplaint — buyer asserts cart contents or complains items missing (not DB status lookup).
func IsCartRecapOrComplaint(userText string, catalog []CatalogItem) bool {
	text := normalizeBuyerTextForRules(userText)
	if text == "" {
		return false
	}
	if IsCheckoutMergeIntent(userText) {
		return false
	}
	complaintSignals := []string{
		"ga masuk", "gak masuk", "belum masuk", "tidak masuk", "blm masuk",
		"harusnya", "kok cuma", "kok baru", "kok malah", "loh ya",
	}
	for _, s := range complaintSignals {
		if strings.Contains(text, s) {
			return true
		}
	}
	if strings.Contains(text, "pesanan") && strings.Contains(text, "ada") {
		if mentionsOrderQty(text) {
			return true
		}
		if countCatalogMatchesInText(text, catalog) >= 2 {
			return true
		}
		if strings.Contains(text, " dan ") || strings.Contains(text, "loh") || strings.Contains(text, "kok") {
			return true
		}
	}
	return false
}

// IsActiveCheckoutRecapQuestion — "apa saya ada pesanan aktif?" merujuk keranjang chat, bukan status DB.
func IsActiveCheckoutRecapQuestion(userText string) bool {
	if parseOrderRefFromMessage(userText) != "" {
		return false
	}
	text := normalizeBuyerTextForRules(userText)
	if text == "" {
		return false
	}
	return strings.Contains(text, "pesanan aktif") || strings.Contains(text, "order aktif")
}

// PreferCheckoutRecapOverDBStatus — klaim "ada pesanan aktif?" harus baca keranjang chat
// (Redis/FSM), bukan FAQ/status DB kosong, selama checkout masih hidup.
func PreferCheckoutRecapOverDBStatus(userText string, hasActiveCheckout bool) bool {
	if !hasActiveCheckout {
		return false
	}
	return IsActiveCheckoutRecapQuestion(userText)
}

// CheckoutStateHasRecap — draft chat punya item yang bisa di-recap (bukan state kosong).
func CheckoutStateHasRecap(st *OrderState) bool {
	if st == nil {
		return false
	}
	n := normalizeOrderState(*st)
	return strings.TrimSpace(n.CatalogItemID) != "" || n.HasMultiItems() || strings.TrimSpace(n.ProductName) != ""
}

// CartRecapReply formats active checkout state for complaint/recap messages.
func CartRecapReply(st OrderState, formal bool) string {
	summary := formatOrderSummary(st)
	if summary == "" {
		if formal {
			return "Mohon maaf kak, belum ada item di keranjang chat ini. Sebut produk + jumlahnya ya."
		}
		return "Maaf kak, keranjang chat ini masih kosong. Sebut produk + jumlahnya ya 🙏"
	}
	prompt := "Ini ringkasan pesanan dari chat ini:"
	if formal {
		prompt = "Berikut ringkasan pesanan dari chat ini:"
	}
	out := prompt + "\n\n" + summary
	if formal {
		out += "\n\nKalau ada item yang kurang, sebut nama produk + jumlahnya ya."
	} else {
		out += "\n\nKalau ada yang kurang, sebut produk + jumlahnya ya kak 🙏"
	}
	return out
}
