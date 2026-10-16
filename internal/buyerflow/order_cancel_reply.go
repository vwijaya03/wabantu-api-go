package buyerflow

import "fmt"

func orderFlowCancelReply(tone, userText string) string {
	idFormal := "Baik kak, ordernya kami batalkan dulu ya. Silakan tanya produk, harga, atau stok jika ada yang dibutuhkan."
	idCasual := "Oke kak, ordernya dibatalkan dulu ya 😊 Mau tanya produk atau harga, langsung chat aja."
	enFormal := "Sure — we've cancelled the order for now. Ask anytime about products, pricing, or stock if you need anything."
	enCasual := "Got it — order cancelled for now 😊 Ask about products or pricing anytime."
	if tone == "formal" {
		return PickBuyerLanguageReply(userText, true, idFormal, enFormal)
	}
	return PickBuyerLanguageReply(userText, false, idCasual, enCasual)
}

// OrderCancelCustomerReply confirms cancellation of a persisted order (DB).
func OrderCancelCustomerReply(userText, tone, ref string) string {
	idBase := fmt.Sprintf("Baik kak, pesanan %s sudah kami batalkan.", ref)
	enBase := fmt.Sprintf("Sure — order %s has been cancelled.", ref)
	idTailFormal := " Silakan tanya produk, harga, atau stok jika ada yang dibutuhkan."
	idTailCasual := " Mau tanya produk atau harga, langsung chat aja 😊"
	enTailFormal := " Ask anytime about products, pricing, or stock if you need anything."
	enTailCasual := " Ask about products or pricing anytime 😊"
	if tone == "formal" {
		return PickBuyerLanguageReply(userText, true, idBase+idTailFormal, enBase+enTailFormal)
	}
	return PickBuyerLanguageReply(userText, false, idBase+idTailCasual, enBase+enTailCasual)
}

// OrderNoneToCancelReply when there is no active order to cancel from chat.
func OrderNoneToCancelReply(userText string) string {
	return orderFlowAbortNoDraftReply("", userText)
}

func orderFlowAbortNoDraftReply(tone, userText string) string {
	idFormal := "Belum ada pesanan aktif kak. Mau lihat katalog atau tanya produk?"
	idCasual := "Belum ada pesanan aktif kak. Mau lihat katalog atau tanya produk?"
	enFormal := "There isn't an active order right now. Want to browse the catalog or ask about a product?"
	enCasual := "No active order right now 😊 Want to see the catalog or ask about a product?"
	if tone == "formal" {
		return PickBuyerLanguageReply(userText, true, idFormal, enFormal)
	}
	return PickBuyerLanguageReply(userText, false, idCasual, enCasual)
}
