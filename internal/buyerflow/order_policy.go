package buyerflow

import "strings"

// IsAddMoreItemsPolicyQuestion — buyer asks whether they can add more items during checkout.
func IsAddMoreItemsPolicyQuestion(userText string) bool {
	text := normalizeBuyerTextForRules(userText)
	if text == "" {
		return false
	}
	phrases := []string{
		"masih mau order", "bisa tambah item", "order item yang lain",
		"tambah item lagi", "bisa order lagi", "mau order lagi",
		"masih bisa order", "bisa tambah produk", "tambah produk lagi",
	}
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// AddMoreItemsPolicyReply explains how to append items during active checkout.
func AddMoreItemsPolicyReply(formal bool, st *OrderState) string {
	base := "Bisa kak. Sebut nama produk + jumlahnya (contoh: lalu abon 250g 1 pcs, atau cadbury mini 1 pcs)."
	if formal {
		base = "Bisa kak. Silakan sebut nama produk dan jumlahnya (contoh: lalu abon 250g 1 pcs)."
	}
	if st == nil || !st.ProductComplete() {
		return base
	}
	summary := formatOrderSummary(*st)
	if summary == "" {
		return base
	}
	return base + "\n\nPesanan saat ini:\n" + summary
}
