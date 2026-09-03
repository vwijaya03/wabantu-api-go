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
		"nambah pesanan", "mau nambah", "tambah pesanan", "bisa nambah",
		"mau tambah", "tambah lagi", "nambah lagi", "mau nambah pesanan",
		"bisa tambah lagi", "masih bisa tambah",
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
	base := "Siap kak, boleh tambah item lagi 😊 Cukup sebut nama produk + jumlahnya ya (contoh: cadbury mini 1 pcs, atau lalu abon 250g 1 pcs)."
	if formal {
		base = "Baik kak, silakan tambah item lagi. Sebut nama produk dan jumlahnya (contoh: cadbury mini 1 pcs)."
	}
	if st == nil || !st.ProductComplete() {
		return base
	}
	summary := formatOrderSummary(*st)
	if summary == "" {
		return base
	}
	return base + "\n\n" + summary
}

func checkoutItemAddedAck(formal bool) string {
	if formal {
		return "Baik kak, item sudah ditambahkan ke pesanan."
	}
	return "Siap kak, sudah ditambahkan ke pesanan ✅"
}

func checkoutAddItemsPolicyReplyIfNeeded(st OrderState, userText string, formal bool) (string, bool) {
	if !IsAddMoreItemsPolicyQuestion(userText) {
		return "", false
	}
	return AddMoreItemsPolicyReply(formal, &st), true
}
