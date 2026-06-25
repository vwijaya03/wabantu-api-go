package ai

import "strings"

// Sales state constants — satu sumber kebenaran routing AI sales.
const (
	SalesStateBrowsing        = "browsing"
	SalesStateConsulting      = "consulting"
	SalesStateProductSelected = "product_selected"
	SalesStateCartReady       = "cart_ready"
	SalesStateCheckout        = "checkout"
	SalesStateCorrection      = "correction"
	SalesStateSensitive       = "sensitive"
	SalesStateOutOfScope      = "out_of_scope"
	SalesStateGreeting        = "greeting"
)

// Sales topic hints untuk catalog / LLM tools.
const (
	SalesTopicList         = "list"
	SalesTopicPrice        = "price"
	SalesTopicRetailPolicy = "retail_policy"
	SalesTopicProduct      = "product"
	SalesTopicGeneral      = "general"
	SalesTopicOrderStatus  = "order_status"
	SalesTopicShipping     = "shipping"
	SalesTopicLocation     = "location"
	SalesTopicRecipient    = "recipient_policy"
)

// SalesIntent — structured intent (poin 3): menggantikan scatter of Is* di router utama.
type SalesIntent struct {
	State       string
	Topic       string
	ProductHint string
	Confidence  float64
}

// ResolveSalesIntent menggabungkan rule-based signals menjadi satu keputusan routing.
func ResolveSalesIntent(
	userText string,
	history []dbMessage,
	orderActive bool,
	inScope bool,
	profile *dbBusinessProfile,
	catalog []dbCatalogItem,
) SalesIntent {
	text := strings.ToLower(strings.TrimSpace(userText))

	for _, kw := range sensitiveKeywords {
		if strings.Contains(text, kw) {
			return SalesIntent{State: SalesStateSensitive, Confidence: 0.98}
		}
	}
	if !inScope {
		return SalesIntent{State: SalesStateOutOfScope, Confidence: 0.9}
	}
	if IsGreetingLike(userText) {
		return SalesIntent{State: SalesStateGreeting, Confidence: 0.9}
	}
	if IsUserSalesCorrection(userText) {
		topic := SalesTopicGeneral
		if IsConsultingPurchaseQuestion(userText, catalog) {
			topic = SalesTopicRetailPolicy
		}
		return SalesIntent{State: SalesStateCorrection, Topic: topic, Confidence: 0.92}
	}
	if IsOrderStatusInquiry(userText) {
		return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicOrderStatus, Confidence: 0.9}
	}
	if IsStructuredOrderList(userText) {
		return SalesIntent{State: SalesStateCartReady, Topic: SalesTopicProduct, Confidence: 0.95}
	}
	if IsExplicitNewOrderStart(userText) {
		hint := productHintFromHistory(history, catalog)
		if hint == "" {
			hint = productHintFromText(userText, catalog)
		}
		return SalesIntent{
			State:       SalesStateCartReady,
			Topic:       SalesTopicProduct,
			ProductHint: hint,
			Confidence:  0.92,
		}
	}
	if IsStoreLocationQuestion(userText) {
		return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicLocation, Confidence: 0.9}
	}
	if IsShippingQuoteQuestion(userText) {
		return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicShipping, Confidence: 0.9}
	}
	if IsComplaintLike(userText) {
		return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicGeneral, Confidence: 0.9}
	}
	if IsHumanEscalationRequest(userText) {
		return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicGeneral, Confidence: 0.9}
	}
	if IsPaymentQuestion(userText) {
		return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicGeneral, Confidence: 0.9}
	}
	if IsMinimumOrderQuestion(userText) {
		return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicRetailPolicy, Confidence: 0.88}
	}
	if IsRecipientPolicyQuestion(userText) {
		return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicRecipient, Confidence: 0.92}
	}
	if IsProductComparisonQuestion(userText) {
		return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicProduct, Confidence: 0.88}
	}
	if IsRecommendationRequest(userText) {
		return SalesIntent{State: SalesStateBrowsing, Topic: SalesTopicList, Confidence: 0.88}
	}
	if IsOrderRevisionMessage(userText) {
		return SalesIntent{State: SalesStateCheckout, Topic: SalesTopicProduct, Confidence: 0.92}
	}
	if orderActive && !ShouldBreakOrderFlow(userText, "ask_variant", catalog) &&
		(IsOrderContinuationMessage(userText) || HasPurchaseIntent(userText)) {
		return SalesIntent{State: SalesStateCheckout, Topic: SalesTopicGeneral, Confidence: 0.88}
	}
	if isNamedProductPurchaseIntent(userText, catalog) {
		match := matchCatalogItem(userText, catalog)
		hint := ""
		if match != nil {
			hint = shortDisplayName(match.Name)
		}
		return SalesIntent{State: SalesStateCartReady, Topic: SalesTopicProduct, ProductHint: hint, Confidence: 0.92}
	}
	if isHistoryBackedPurchaseIntent(userText, history, catalog) {
		match := matchCatalogFromFocusedHistory(history, catalog)
		hint := ""
		if match != nil {
			hint = shortDisplayName(match.Name)
		}
		return SalesIntent{State: SalesStateCartReady, Topic: SalesTopicProduct, ProductHint: hint, Confidence: 0.92}
	}
	if hasPurchaseIntent(userText, catalog) {
		if IsOffBusinessProductRequest(userText, businessScopeKeywords(profile)) {
			return SalesIntent{State: SalesStateOutOfScope, Confidence: 0.92}
		}
		hint := productHintFromText(userText, catalog)
		return SalesIntent{State: SalesStateCartReady, Topic: SalesTopicProduct, ProductHint: hint, Confidence: 0.9}
	}
	if IsCatalogBrowsingIntent(userText) || isGeneralStoreCatalogQuestion(userText) {
		return SalesIntent{State: SalesStateBrowsing, Topic: SalesTopicList, Confidence: 0.88}
	}
	if IsConsultingPurchaseQuestion(userText, catalog) {
		hint := productHintFromHistory(history, catalog)
		if hint == "" {
			hint = productHintFromText(userText, catalog)
		}
		return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicRetailPolicy, ProductHint: hint, Confidence: 0.9}
	}
	if IsPricingUnitClarification(userText) || isCatalogContextualReference(userText) {
		hint := productHintFromHistory(history, catalog)
		return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicPrice, ProductHint: hint, Confidence: 0.88}
	}
	if IsProductSellInquiry(userText, catalog) {
		hint := productHintFromText(userText, catalog)
		return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicProduct, ProductHint: hint, Confidence: 0.88}
	}
	if IsCatalogProductInquiry(userText) {
		hint := productHintFromText(userText, catalog)
		if hint == "" {
			hint = productHintFromHistory(history, catalog)
		}
		topic := SalesTopicProduct
		if strings.Contains(text, "harga") || strings.Contains(text, "berapa") {
			topic = SalesTopicPrice
		}
		return SalesIntent{State: SalesStateConsulting, Topic: topic, ProductHint: hint, Confidence: 0.85}
	}
	if match := matchCatalogItem(userText, catalog); match != nil && !IsQuestionLike(userText) {
		return SalesIntent{
			State:       SalesStateProductSelected,
			Topic:       SalesTopicProduct,
			ProductHint: shortDisplayName(match.Name),
			Confidence:  0.8,
		}
	}
	if IsQuestionLike(userText) {
		return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicGeneral, Confidence: 0.75}
	}
	return SalesIntent{State: SalesStateConsulting, Topic: SalesTopicGeneral, Confidence: 0.7}
}

func productHintFromText(userText string, catalog []dbCatalogItem) string {
	if match := matchCatalogItem(userText, catalog); match != nil {
		return shortDisplayName(match.Name)
	}
	return ""
}

func productHintFromHistory(history []dbMessage, catalog []dbCatalogItem) string {
	if match := matchCatalogFromFocusedHistory(history, catalog); match != nil {
		return shortDisplayName(match.Name)
	}
	return ""
}

// salesIntentToClassifier maps SalesIntent ke label classifier lama (kompatibilitas).
func salesIntentToClassifier(intent SalesIntent) classifyResult {
	switch intent.State {
	case SalesStateSensitive:
		return classifyResult{Label: "sensitive_escalate", Confidence: intent.Confidence}
	case SalesStateOutOfScope:
		return classifyResult{Label: "out_of_scope", Confidence: intent.Confidence}
	case SalesStateCartReady, SalesStateCheckout:
		return classifyResult{Label: "order_intent", Confidence: intent.Confidence}
	case SalesStateGreeting:
		return classifyResult{Label: "in_scope_non_question", Confidence: intent.Confidence}
	case SalesStateBrowsing, SalesStateConsulting, SalesStateProductSelected, SalesStateCorrection:
		return classifyResult{Label: "in_scope_question", Confidence: intent.Confidence}
	default:
		return classifyResult{Label: "in_scope_question", Confidence: intent.Confidence}
	}
}
