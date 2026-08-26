package buyerflow

// PersistOrderFunc — callback when checkout FSM completes (production: DB persist).
type PersistOrderFunc func(st OrderState) (string, error)

// OrderFlowTemplates — default WA copy; overridden by KB when matched.
type OrderFlowTemplates = orderFlowTemplates

func StrOrEmpty(s *string) string { return strOrEmpty(s) }

func StrPtr(s string) *string { return strPtr(s) }

func OutOfScopeReply(profile *BusinessProfile) string { return outOfScopeReply(profile) }

func BusinessScopeKeywords(profile *BusinessProfile) []string { return businessScopeKeywords(profile) }

func NewOmahSimulator() *Simulator { return newOmahSimulator() }

func NormalizeOrderState(st OrderState) OrderState { return normalizeOrderState(st) }

func OrderTemplatesFromKB(kb []KBEntry, formal bool) OrderFlowTemplates {
	return orderTemplatesFromKB(kb, formal)
}

func HasPurchaseIntentWithCatalog(userText string, catalog []CatalogItem) bool {
	return hasPurchaseIntent(userText, catalog)
}

func MatchCatalogItem(userText string, catalog []CatalogItem) *CatalogItem {
	return matchCatalogItem(userText, catalog)
}

func ReplyFromBusinessCatalog(userText string, profile *BusinessProfile, catalog []CatalogItem, history []Message) (string, bool) {
	return replyFromBusinessCatalog(userText, profile, catalog, history)
}

func TryPaymentFAQAnswer(query string, kb []KBEntry) (string, bool) {
	return tryPaymentFAQAnswer(query, kb)
}

func ReplyRecipientPolicyQuestion(userText string, kb []KBEntry, formal bool) string {
	return replyRecipientPolicyQuestion(userText, kb, formal)
}

func ThirdPartyBuyerLookupDeniedReply() string { return thirdPartyBuyerLookupDeniedReply() }

func OrderFlowCancelReply(tone string) string { return orderFlowCancelReply(tone) }

func OrderFlowLoopBreakReply(formal bool) string { return orderFlowLoopBreakReply(formal) }

func SalesIntentToClassifier(intent SalesIntent) ClassifyResult {
	return salesIntentToClassifier(intent)
}

func IsCommerceDominant(userText string) bool { return isCommerceDominant(userText) }

func GuardOrderQtyStep(st OrderState, catalog []CatalogItem, formal bool, qtyStep string) (OrderState, string, bool) {
	return guardOrderQtyStep(st, catalog, formal, qtyStep)
}

func WantsOrderContextFromHistory(userText string) bool { return wantsOrderContextFromHistory(userText) }

func PrependSalesCorrection(formal bool, body string) string { return prependSalesCorrection(formal, body) }

func SalesCorrectionReply(formal bool) string { return salesCorrectionReply(formal) }

func BuildVariantLabel(size, color string) string { return buildVariantLabel(size, color) }

func MentionsOrderQty(text string) bool { return mentionsOrderQty(text) }

func HasOrderIntentText(userText string) bool { return hasOrderIntentText(userText) }

func CasualPraiseReply(formal bool) string { return casualPraiseReply(formal) }

func TopKBMatchScore(query string, kb []KBEntry) float64 { return topKBMatchScore(query, kb) }

func TryFAQDirectAnswer(query string, kb []KBEntry) (string, bool) { return tryFAQDirectAnswer(query, kb) }

func IsGeneralStoreCatalogQuestion(userText string) bool { return isGeneralStoreCatalogQuestion(userText) }

func CatalogConfirmLine(st OrderState) string { return catalogConfirmLine(st) }

func FormatUpsellBlock(st OrderState, catalog []CatalogItem) string { return formatUpsellBlock(st, catalog) }

type ParsedOrderHints = parsedOrderHints

func ParseOrderHints(userText string) ParsedOrderHints { return parseOrderHints(userText) }

func ApplyCatalogMatch(st *OrderState, match *CatalogItem) { applyCatalogMatch(st, match) }

func ParseOrderQty(text string) (int, bool) { return parseOrderQty(text) }

func InferVariantFromProductName(st *OrderState) { inferVariantFromProductName(st) }

func ParseSizeAndColor(text string) (string, string) { return parseSizeAndColor(text) }

func FormatCatalogPicker(catalog []CatalogItem, limit int) string { return formatCatalogPicker(catalog, limit) }

func FormatOrderSummary(st OrderState) string { return formatOrderSummary(st) }

func MatchCatalogFromFocusedHistory(history []Message, catalog []CatalogItem) *CatalogItem {
	return matchCatalogFromFocusedHistory(history, catalog)
}

func ParseOrderRefFromMessage(userText string) string { return parseOrderRefFromMessage(userText) }

func OrderRecipientHintNotFoundReply() string { return orderRecipientHintNotFoundReply() }

func FullAddressBlock() string { return fullAddressBlock() }

func RecipientBlock(name, phone string) string { return recipientBlock(name, phone) }

func OrderSizeLineMatches(text string) bool { return orderSizeLineRe.MatchString(text) }

func OmahProfile() *BusinessProfile { return omahProfile() }

func OmahCatalog() []CatalogItem { return omahCatalog() }

func TryApplyQtyRevision(st *OrderState, userText string) bool { return tryApplyQtyRevision(st, userText) }

func MergeShippingText(st *OrderState, userText string) { mergeShippingText(st, userText) }

func ParseCatalogPriceInfo(it *CatalogItem) CatalogPriceInfo { return parseCatalogPriceInfo(it) }

func FormatCatalogPrice(it *CatalogItem) string { return formatCatalogPrice(it) }

func ApplyLineToOrderState(st *OrderState, line OrderLineState) { applyLineToOrderState(st, line) }

func CatalogItemNeedsVariant(it *CatalogItem) bool { return catalogItemNeedsVariant(it) }

func BuildCatalogItemReply(formal bool, it *CatalogItem, qty int) string {
	return buildCatalogItemReply(formal, it, qty)
}

func ParseOrderRefFromHistory(history []Message) string { return parseOrderRefFromHistory(history) }

func NormalizePhoneID(p string) string { return normalizePhoneID(p) }

func HasRecipientHintInMessage(userText string) bool { return hasRecipientHintInMessage(userText) }

func ParseRecipientHintFromMessage(userText string) (string, string) {
	return parseRecipientHintFromMessage(userText)
}

func StripOrderSizeTokens(text string) string {
	cleaned := orderSizeLineRe.ReplaceAllString(text, "")
	cleaned = orderQtyLusinRe.ReplaceAllString(cleaned, "")
	return orderQtyWithUnitRe.ReplaceAllString(cleaned, "")
}

func ShortDisplayName(name string) string { return shortDisplayName(name) }

func FormatMoney(amount float64) string { return formatMoney(amount) }

func OrderCompleteMessageWithRef(orderID string, st OrderState, tmpl OrderFlowTemplates) string {
	return orderCompleteMessageWithRef(orderID, st, tmpl)
}

func Tokenize(text string) []string { return tokenize(text) }

func OverlapScore(a, b []string) float64 { return overlapScore(a, b) }

func ResolveOrderWarehouse(lines []CatalogStockLine, qty int, preferredWarehouseID string) (string, bool) {
	return resolveOrderWarehouse(lines, qty, preferredWarehouseID)
}

func StockQtyRejectReply(st OrderState, lines []CatalogStockLine, formal bool) string {
	return stockQtyRejectReply(st, lines, formal)
}
