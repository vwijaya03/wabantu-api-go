package buyerflow

import "strings"

// AiNoAnswerPauseReason — pause AI; distinct from buyer_requested_human in inbox.
const AiNoAnswerPauseReason = "ai_no_answer"

// NoAnswerHandoffReply — honest handoff; no invented SLA.
func NoAnswerHandoffReply(formal bool, userText string) string {
	idFormal := "Maaf kak, untuk pertanyaan ini saya belum punya jawaban yang pasti dari katalog atau data pesanan. Tim kami akan melanjutkan chat ini sebentar lagi ya 🙏"
	idCasual := "Maaf kak, aku belum nemu jawaban yang tepat dari katalog/data pesanan. Tim kami lanjutin chat ini sebentar lagi ya 🙏"
	enFormal := "Sorry — I don't have a reliable answer from our catalog or order records for this yet. A teammate will continue this chat shortly 🙏"
	enCasual := "Sorry — I don't have a solid answer from our catalog or order info for this yet. Someone from our team will jump in shortly 🙏"
	if formal {
		return PickBuyerLanguageReply(userText, formal, idFormal, enFormal)
	}
	return PickBuyerLanguageReply(userText, formal, idCasual, enCasual)
}

// IsClearProductBrowse — list/catalog browse; keep grounded catalog replies.
func IsClearProductBrowse(userText string) bool {
	if IsCatalogBrowsingIntent(userText) || IsCatalogListQuestion(userText) ||
		IsRecommendationRequest(userText) || IsCatalogExclusionQuestion(userText) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	browse := []string{
		"show catalog", "show me products", "what do you sell", "product list",
		"list products", "browse catalog", "see the menu",
	}
	for _, p := range browse {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// ShouldHandoffForCatalogMiss — product question with no match; not a browse/list request.
func ShouldHandoffForCatalogMiss(userText string) bool {
	if IsClearProductBrowse(userText) {
		return false
	}
	if IsShippingFAQQuestion(userText) || IsPaymentQuestion(userText) ||
		IsOrderStatusInquiry(userText) || IsRecipientPolicyQuestion(userText) {
		return false
	}
	if IsCatalogProductInquiry(userText) || IsProductSellInquiry(userText, nil) ||
		looksLikeNamedProductSellInquiry(userText) {
		return true
	}
	return IsQuestionLike(userText)
}

// IsSideConsultDuringCheckout — unrelated in-scope question mid-checkout (not amend/cancel/recap/data).
func IsSideConsultDuringCheckout(userText string) bool {
	if !IsQuestionLike(userText) || HasCheckoutDataInMessage(userText) {
		return false
	}
	if IsCartLineCorrectionIntent(userText) || IsOrderAmendMessage(userText) ||
		IsOrderRevisionMessage(userText) || IsOrderCancelRequest(userText) ||
		IsNegatedFullOrderCancel(userText) || IsCheckoutMergeIntent(userText) ||
		IsActiveCheckoutRecapQuestion(userText) || IsCartRecapOrComplaint(userText, nil) ||
		hasPurchaseIntent(userText, nil) || IsStructuredOrderList(userText) {
		return false
	}
	return true
}

// ShouldNoAnswerHandoff — replace scolding/fallback for in-scope questions we cannot ground.
func ShouldNoAnswerHandoff(userText string, inScope bool) bool {
	if IsClearProductBrowse(userText) || IsRecipientPolicyQuestion(userText) ||
		IsHumanEscalationRequest(userText) || IsShippingFAQQuestion(userText) ||
		IsPaymentQuestion(userText) || IsOrderStatusInquiry(userText) {
		return false
	}
	if !IsQuestionLike(userText) {
		return false
	}
	return true
}

// IsCatalogNotFoundReply detects deterministic catalog miss copy (not a product hit).
func IsCatalogNotFoundReply(reply string) bool {
	lower := strings.ToLower(reply)
	return strings.Contains(lower, "belum ketemu") ||
		strings.Contains(lower, "belum kami temukan") ||
		strings.Contains(lower, "not in our catalog") ||
		strings.Contains(lower, "couldn't find that product")
}
