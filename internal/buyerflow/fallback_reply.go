package buyerflow

import "strings"

// ScopeDirectionReplyFor — arahkan pembeli ke topik bisnis (in_scope_non_question / auto_fallback).
func ScopeDirectionReplyFor(userText, productsServices string, formal bool) string {
	scope := strings.TrimSpace(productsServices)
	idEmpty := "Boleh kak, biar tepat, bisa tanya seputar produk, harga, stok, atau pengiriman dari bisnis kami ya."
	enEmpty := "Happy to help — ask about our products, prices, stock, or shipping."
	idScope := "Boleh kak, biar tepat kami bantu pertanyaan seputar: " + scope + ". Bisa tanya harga/stok/order ya."
	enScope := "We can help with: " + scope + ". Ask about price, stock, or placing an order."
	if scope == "" {
		return PickBuyerLanguageReply(userText, formal, idEmpty, enEmpty)
	}
	return PickBuyerLanguageReply(userText, formal, idScope, enScope)
}

// NonAiDefaultReplyFor — batas kuota / eskalasi non-LLM.
func NonAiDefaultReplyFor(userText, productsServices string, formal bool) string {
	scope := strings.TrimSpace(productsServices)
	idEmpty := "Maaf kak, tim CS kami akan segera menghubungi kakak untuk bantu lebih lanjut ya 🙏"
	enEmpty := "Sorry — our team will reach out shortly to help you further 🙏"
	idScope := "Maaf kak, tim CS kami akan segera menghubungi kakak. Saat ini kami fokus bantu seputar: " + scope + "."
	enScope := "Sorry — our team will contact you soon. We focus on: " + scope + "."
	if scope == "" {
		return PickBuyerLanguageReply(userText, formal, idEmpty, enEmpty)
	}
	return PickBuyerLanguageReply(userText, formal, idScope, enScope)
}

// SensitiveEscalateReplyFor — keyword sensitif; pause AI.
func SensitiveEscalateReplyFor(userText string, formal bool) string {
	id := "Maaf kak, untuk topik ini tim CS kami akan langsung mengambil alih dan segera menghubungi kakak 🙏"
	en := "Sorry — for this topic our team will take over and contact you shortly 🙏"
	return PickBuyerLanguageReply(userText, formal, id, en)
}
