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
		"can i add more items", "add more items", "can i add another item",
		"add another item", "order more items", "add more to my order",
	}
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// AddMoreItemsPolicyReply explains how to append items during active checkout.
func AddMoreItemsPolicyReply(formal bool, st *OrderState, userText string) string {
	idCasual := "Siap kak, boleh tambah item lagi 😊 Cukup sebut nama produk + jumlahnya ya (contoh: nama SKU 1 pcs)."
	idFormal := "Baik kak, silakan tambah item lagi. Sebut nama produk dan jumlahnya (contoh: nama SKU 1 pcs)."
	enCasual := "Sure — you can add more items 😊 Just send the product name and quantity (e.g. product name 1 pc)."
	enFormal := "Of course — you can add more items. Send the product name and quantity (e.g. product name 1 pc)."
	var base string
	if formal {
		base = PickBuyerLanguageReply(userText, true, idFormal, enFormal)
	} else {
		base = PickBuyerLanguageReply(userText, false, idCasual, enCasual)
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

func checkoutItemAddedAck(formal bool, userText string) string {
	idCasual := "Siap kak, sudah ditambahkan ke pesanan ✅"
	idFormal := "Baik kak, item sudah ditambahkan ke pesanan."
	enCasual := "Got it — added to your order ✅"
	enFormal := "Done — the item has been added to your order."
	if formal {
		return PickBuyerLanguageReply(userText, true, idFormal, enFormal)
	}
	return PickBuyerLanguageReply(userText, false, idCasual, enCasual)
}

func checkoutAddItemsPolicyReplyIfNeeded(st OrderState, userText string, catalog []CatalogItem, formal bool) (string, bool) {
	if !IsStandaloneAddMoreItemsPolicyQuestion(userText, catalog) {
		return "", false
	}
	return AddMoreItemsPolicyReply(formal, &st, userText), true
}

// IsStandaloneAddMoreItemsPolicyQuestion — "boleh nambah?" tanpa menyebut SKU/merek.
func IsStandaloneAddMoreItemsPolicyQuestion(userText string, catalog []CatalogItem) bool {
	if !IsAddMoreItemsPolicyQuestion(userText) {
		return false
	}
	return !catalogSKUOrBrandIntent(userText, catalog)
}

func catalogSKUOrBrandIntent(userText string, catalog []CatalogItem) bool {
	if strings.TrimSpace(userText) == "" || len(catalog) == 0 {
		return false
	}
	if lexicalBrandAmbiguous(userText, catalog) {
		return true
	}
	if uniqueBrandSKUFromText(userText, catalog) != nil {
		return true
	}
	if resolveOrderProductMatch(userText, nil, catalog, nil) != nil {
		return true
	}
	return brandTokenFromText(userText, catalog) != ""
}
