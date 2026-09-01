package ai

import bf "encore.app/wabantu/internal/buyerflow"

// Types — shared dengan internal/buyerflow (satu sumber kebenaran routing).
type (
	ConversationSimulator = bf.Simulator
	TurnOutcome           = bf.TurnOutcome
	OrderFlowInput        = bf.OrderFlowInput
	OrderFlowResult       = bf.OrderFlowResult
	dbBusinessProfile     = bf.BusinessProfile
	dbMessage             = bf.Message
	dbKBEntry             = bf.KBEntry
	dbCatalogItem         = bf.CatalogItem
	catalogStockLine      = bf.CatalogStockLine
	orderState            = bf.OrderState
	orderLineState        = bf.OrderLineState
	classifyResult        = bf.ClassifyResult
	SalesIntent           = bf.SalesIntent
	MessageComplexity     = bf.MessageComplexity
)

type persistOrderFunc = bf.PersistOrderFunc

// Sales state / topic constants.
const (
	SalesStateBrowsing        = bf.SalesStateBrowsing
	SalesStateConsulting      = bf.SalesStateConsulting
	SalesStateProductSelected = bf.SalesStateProductSelected
	SalesStateCartReady       = bf.SalesStateCartReady
	SalesStateCheckout        = bf.SalesStateCheckout
	SalesStateCorrection      = bf.SalesStateCorrection
	SalesStateSensitive       = bf.SalesStateSensitive
	SalesStateOutOfScope      = bf.SalesStateOutOfScope
	SalesStateGreeting        = bf.SalesStateGreeting
	SalesTopicList            = bf.SalesTopicList
	SalesTopicPrice           = bf.SalesTopicPrice
	SalesTopicRetailPolicy    = bf.SalesTopicRetailPolicy
	SalesTopicProduct         = bf.SalesTopicProduct
	SalesTopicGeneral         = bf.SalesTopicGeneral
	SalesTopicOrderStatus     = bf.SalesTopicOrderStatus
	SalesTopicShipping        = bf.SalesTopicShipping
	SalesTopicLocation        = bf.SalesTopicLocation
	SalesTopicRecipient       = bf.SalesTopicRecipient
	ComplexitySimple          = bf.ComplexitySimple
	ComplexityComplex         = bf.ComplexityComplex
)

// Delivery path constants.
const (
	PathProfileIncomplete = bf.PathProfileIncomplete
	PathGreeting          = bf.PathGreeting
	PathInjectionGuard    = bf.PathInjectionGuard
	PathEscalate          = bf.PathEscalate
	PathOutOfScope        = bf.PathOutOfScope
	PathNonQuestion       = bf.PathNonQuestion
	PathLowConfidence     = bf.PathLowConfidence
	PathOrderFlow         = bf.PathOrderFlow
	PathOrderCancel       = bf.PathOrderCancel
	PathOrderStatus       = bf.PathOrderStatus
	PathOrderLookupDenied = bf.PathOrderLookupDenied
	PathRecipientPolicy   = bf.PathRecipientPolicy
	PathCatalogDB         = bf.PathCatalogDB
	PathConsulting        = bf.PathConsulting
	PathFAQCache          = bf.PathFAQCache
	PathFAQDirect         = bf.PathFAQDirect
	PathLLM               = bf.PathLLM
	PathLLMTools          = bf.PathLLMTools
	PathLLMGrounded       = bf.PathLLMGrounded
	PathCostLimit         = bf.PathCostLimit
	PathAutoFallback      = bf.PathAutoFallback
	PathPaymentProof      = bf.PathPaymentProof
	PathPaymentFAQ        = bf.PathPaymentFAQ
	PathShippingFAQ       = bf.PathShippingFAQ
	PathProductImageMatch = bf.PathProductImageMatch
	PathImageFallback     = bf.PathImageFallback
)

// Exported routing functions — delegate ke buyerflow.
var (
	AdvanceOrderFlow              = bf.AdvanceOrderFlow
	BuildCatalogContext           = bf.BuildCatalogContext
	CatalogStockLinesForItem      = bf.CatalogStockLinesForItem
	ClassifyComplexity            = bf.ClassifyComplexity
	CurrentGreetingPeriodWIB      = bf.CurrentGreetingPeriodWIB
	DetectGreetingPeriodFromText  = bf.DetectGreetingPeriodFromText
	ExtractScopeKeywords          = bf.ExtractScopeKeywords
	FAQDirectGuardsPass             = bf.FAQDirectGuardsPass
	GreetingFeedbackReply         = bf.GreetingFeedbackReply
	GreetingReply                 = bf.GreetingReply
	HasPurchaseIntent             = bf.HasPurchaseIntent
	IsAbandonedCartSignal         = bf.IsAbandonedCartSignal
	IsAcknowledgmentLike          = bf.IsAcknowledgmentLike
	IsActiveCheckoutFromHistory   = bf.IsActiveCheckoutFromHistory
	IsAmbiguousPurchaseSignal     = bf.IsAmbiguousPurchaseSignal
	IsCancelClarificationQuestion = bf.IsCancelClarificationQuestion
	IsCasualChatOpener            = bf.IsCasualChatOpener
	IsCasualPraiseLike            = bf.IsCasualPraiseLike
	IsCatalogBrowsingIntent       = bf.IsCatalogBrowsingIntent
	IsCatalogListQuestion         = bf.IsCatalogListQuestion
	IsCatalogProductInquiry       = bf.IsCatalogProductInquiry
	IsComplaintLike               = bf.IsComplaintLike
	IsConsultingPurchaseQuestion  = bf.IsConsultingPurchaseQuestion
	IsDraftOrderCancelRequest     = bf.IsDraftOrderCancelRequest
	IsExplicitNewOrderStart       = bf.IsExplicitNewOrderStart
	IsExplicitPersistedOrderCancel = bf.IsExplicitPersistedOrderCancel
	IsGreetingFeedback            = bf.IsGreetingFeedback
	IsGreetingLike                = bf.IsGreetingLike
	IsHumanEscalationRequest      = bf.IsHumanEscalationRequest
	IsMinimumOrderQuestion        = bf.IsMinimumOrderQuestion
	IsNewPurchaseIntentQuestion   = bf.IsNewPurchaseIntentQuestion
	IsOffBusinessProductRequest   = bf.IsOffBusinessProductRequest
	IsOrderCancelRequest          = bf.IsOrderCancelRequest
	IsOrderContinuationMessage    = bf.IsOrderContinuationMessage
	IsOrderFlowCancelled          = bf.IsOrderFlowCancelled
	IsOrderFollowUpFromHistory    = bf.IsOrderFollowUpFromHistory
	IsOrderRefStatusLookup        = bf.IsOrderRefStatusLookup
	IsOrderRevisionMessage        = bf.IsOrderRevisionMessage
	IsOrderStatusInquiry          = bf.IsOrderStatusInquiry
	IsOrderTotalRequest           = bf.IsOrderTotalRequest
	IsPaymentQuestion             = bf.IsPaymentQuestion
	IsPaymentRejectionInquiry     = bf.IsPaymentRejectionInquiry
	IsPaymentStatusInquiry        = bf.IsPaymentStatusInquiry
	IsPricingUnitClarification    = bf.IsPricingUnitClarification
	IsProductComparisonQuestion   = bf.IsProductComparisonQuestion
	IsProductSellInquiry          = bf.IsProductSellInquiry
	IsPromptInjectionLikely       = bf.IsPromptInjectionLikely
	IsQuestionLike                = bf.IsQuestionLike
	IsRecipientPolicyQuestion     = bf.IsRecipientPolicyQuestion
	IsRecommendationRequest       = bf.IsRecommendationRequest
	IsSelfBuyerOrderLookup        = bf.IsSelfBuyerOrderLookup
	IsShippingQuoteQuestion       = bf.IsShippingQuoteQuestion
	IsShippingFAQQuestion         = bf.IsShippingFAQQuestion
	IsSoftCancelRegret            = bf.IsSoftCancelRegret
	IsStoreLocationQuestion       = bf.IsStoreLocationQuestion
	IsStructuredOrderList         = bf.IsStructuredOrderList
	IsThirdPartyBuyerLookup       = bf.IsThirdPartyBuyerLookup
	IsUserSalesCorrection         = bf.IsUserSalesCorrection
	IsWithinBusinessScope         = bf.IsWithinBusinessScope
	ResolveSalesIntent            = bf.ResolveSalesIntent
	SanitizeForPrompt             = bf.SanitizeForPrompt
	ShouldBreakOrderFlow          = bf.ShouldBreakOrderFlow
	ShouldCancelPersistedOrder    = bf.ShouldCancelPersistedOrder
	WantsActiveOrderOnly          = bf.WantsActiveOrderOnly
)

// FormatOrderNumber — tetap delegasi ke order package (production DB layer).
func FormatOrderNumber(orderID string) string { return bf.FormatOrderNumber(orderID) }

func strOrEmpty(s *string) string { return bf.StrOrEmpty(s) }

func strPtr(s string) *string { return bf.StrPtr(s) }

func outOfScopeReply(profile *dbBusinessProfile) string { return bf.OutOfScopeReply(profile) }

func businessScopeKeywords(profile *dbBusinessProfile) []string { return bf.BusinessScopeKeywords(profile) }

func newOmahSimulator() *ConversationSimulator { return bf.NewOmahSimulator() }

func normalizeOrderState(st orderState) orderState { return bf.NormalizeOrderState(st) }

func orderTemplatesFromKB(kb []dbKBEntry, formal bool) orderFlowTemplates {
	return bf.OrderTemplatesFromKB(kb, formal)
}

func hasPurchaseIntent(userText string, catalog []dbCatalogItem) bool {
	return bf.HasPurchaseIntentWithCatalog(userText, catalog)
}

func matchCatalogItem(userText string, catalog []dbCatalogItem) *dbCatalogItem {
	return bf.MatchCatalogItem(userText, catalog)
}

func replyFromBusinessCatalog(userText string, profile *dbBusinessProfile, catalog []dbCatalogItem, history []dbMessage) (string, bool) {
	return bf.ReplyFromBusinessCatalog(userText, profile, catalog, history)
}

func tryPaymentFAQAnswer(query string, kb []dbKBEntry) (string, bool) {
	return bf.TryPaymentFAQAnswer(query, kb)
}

func tryShippingFAQReply(userText string, profile *dbBusinessProfile, kb []dbKBEntry, formal bool) (string, bool) {
	return bf.TryShippingFAQReply(userText, profile, kb, formal)
}

func replyRecipientPolicyQuestion(userText string, kb []dbKBEntry, formal bool) string {
	return bf.ReplyRecipientPolicyQuestion(userText, kb, formal)
}

func thirdPartyBuyerLookupDeniedReply() string { return bf.ThirdPartyBuyerLookupDeniedReply() }

func orderFlowCancelReply(tone string) string { return bf.OrderFlowCancelReply(tone) }

func orderFlowLoopBreakReply(formal bool) string { return bf.OrderFlowLoopBreakReply(formal) }

func salesIntentToClassifier(intent SalesIntent) classifyResult {
	return bf.SalesIntentToClassifier(intent)
}

func isCommerceDominant(userText string) bool { return bf.IsCommerceDominant(userText) }

func guardOrderQtyStep(st orderState, catalog []dbCatalogItem, formal bool, qtyStep string) (orderState, string, bool) {
	return bf.GuardOrderQtyStep(st, catalog, formal, qtyStep)
}

func wantsOrderContextFromHistory(userText string) bool { return bf.WantsOrderContextFromHistory(userText) }

func prependSalesCorrection(formal bool, body string) string { return bf.PrependSalesCorrection(formal, body) }

func salesCorrectionReply(formal bool) string { return bf.SalesCorrectionReply(formal) }

func buildVariantLabel(size, color string) string { return bf.BuildVariantLabel(size, color) }

func mentionsOrderQty(text string) bool { return bf.MentionsOrderQty(text) }

func hasOrderIntentText(userText string) bool { return bf.HasOrderIntentText(userText) }

func casualPraiseReply(formal bool) string { return bf.CasualPraiseReply(formal) }

func topKBMatchScore(query string, kb []dbKBEntry) float64 { return bf.TopKBMatchScore(query, kb) }

func tryFAQDirectAnswer(query string, kb []dbKBEntry) (string, bool) { return bf.TryFAQDirectAnswer(query, kb) }

func isGeneralStoreCatalogQuestion(userText string) bool { return bf.IsGeneralStoreCatalogQuestion(userText) }

func catalogConfirmLine(st orderState) string { return bf.CatalogConfirmLine(st) }

func formatUpsellBlock(st orderState, catalog []dbCatalogItem) string { return bf.FormatUpsellBlock(st, catalog) }

type parsedOrderHints = bf.ParsedOrderHints

func parseOrderHints(userText string) parsedOrderHints { return bf.ParseOrderHints(userText) }

func applyCatalogMatch(st *orderState, match *dbCatalogItem) { bf.ApplyCatalogMatch(st, match) }

func parseOrderQty(text string) (int, bool) { return bf.ParseOrderQty(text) }

func inferVariantFromProductName(st *orderState) { bf.InferVariantFromProductName(st) }

func parseSizeAndColor(text string) (string, string) { return bf.ParseSizeAndColor(text) }

func formatCatalogPicker(catalog []dbCatalogItem, limit int) string { return bf.FormatCatalogPicker(catalog, limit) }

func formatOrderSummary(st orderState) string { return bf.FormatOrderSummary(st) }

func matchCatalogFromFocusedHistory(history []dbMessage, catalog []dbCatalogItem) *dbCatalogItem {
	return bf.MatchCatalogFromFocusedHistory(history, catalog)
}

func orderCompleteMessageWithRef(orderID string, st orderState, tmpl orderFlowTemplates) string {
	return bf.OrderCompleteMessageWithRef(orderID, st, tmpl)
}

func parseOrderRefFromMessage(userText string) string { return bf.ParseOrderRefFromMessage(userText) }

func orderRecipientHintNotFoundReply() string { return bf.OrderRecipientHintNotFoundReply() }

func recipientBlock(name, phone string) string { return bf.RecipientBlock(name, phone) }

func fullAddressBlock() string { return bf.FullAddressBlock() }

func orderSizeLineMatches(text string) bool { return bf.OrderSizeLineMatches(text) }

func omahProfile() *dbBusinessProfile { return bf.OmahProfile() }

func omahCatalog() []dbCatalogItem { return bf.OmahCatalog() }

func tryApplyQtyRevision(st *orderState, userText string) bool { return bf.TryApplyQtyRevision(st, userText) }

func mergeShippingText(st *orderState, userText string) { bf.MergeShippingText(st, userText) }

func parseCatalogPriceInfo(it *dbCatalogItem) catalogPriceInfo { return bf.ParseCatalogPriceInfo(it) }

func formatCatalogPrice(it *dbCatalogItem) string { return bf.FormatCatalogPrice(it) }

func catalogItemNeedsVariant(it *dbCatalogItem) bool { return bf.CatalogItemNeedsVariant(it) }

func buildCatalogItemReply(formal bool, it *dbCatalogItem, qty int) string {
	return bf.BuildCatalogItemReply(formal, it, qty)
}

func parseOrderRefFromHistory(history []dbMessage) string { return bf.ParseOrderRefFromHistory(history) }

func normalizePhoneID(p string) string { return bf.NormalizePhoneID(p) }

func hasRecipientHintInMessage(userText string) bool { return bf.HasRecipientHintInMessage(userText) }

func parseRecipientHintFromMessage(userText string) (string, string) {
	return bf.ParseRecipientHintFromMessage(userText)
}

func shortDisplayName(name string) string { return bf.ShortDisplayName(name) }

func formatMoney(amount float64) string { return bf.FormatMoney(amount) }

type catalogPriceInfo = bf.CatalogPriceInfo
func tokenize(text string) []string { return bf.Tokenize(text) }

func overlapScore(a, b []string) float64 { return bf.OverlapScore(a, b) }

func resolveOrderWarehouse(lines []catalogStockLine, qty int, preferredWarehouseID string) (string, bool) {
	return bf.ResolveOrderWarehouse(lines, qty, preferredWarehouseID)
}

func stockQtyRejectReply(st orderState, lines []catalogStockLine, formal bool) string {
	return bf.StockQtyRejectReply(st, lines, formal)
}

type orderFlowTemplates = bf.OrderFlowTemplates

func resolveCatalogMatch(userText string, history []dbMessage, catalog []dbCatalogItem) *dbCatalogItem {
	return bf.ResolveCatalogMatch(userText, history, catalog)
}

const catalogEmptyMarker = bf.CatalogEmptyMarker

var orderAddrHintRe = bf.OrderAddrHintRe

func formatStockLabel(qty float64) string { return bf.FormatStockLabel(qty) }

func buildCatalogListReply(formal bool, bizName string, catalog []dbCatalogItem, profile *dbBusinessProfile) string {
	return bf.BuildCatalogListReply(formal, bizName, catalog, profile)
}

func warehouseBuyerLabel(customerLabel, warehouseName string) string {
	return bf.WarehouseBuyerLabel(customerLabel, warehouseName)
}

func validateOrderQtyAgainstStock(st orderState, catalog []dbCatalogItem, formal bool) (bool, string, string) {
	return bf.ValidateOrderQtyAgainstStock(st, catalog, formal)
}

func parseRecipientLine(userText string) (string, string) {
	return bf.ParseRecipientLine(userText)
}

func isHistoryBackedPurchaseIntent(userText string, history []dbMessage, catalog []dbCatalogItem) bool {
	return bf.IsHistoryBackedPurchaseIntent(userText, history, catalog)
}

func wouldRepeatOutbound(history []dbMessage, outbound string) bool {
	return bf.WouldRepeatOutbound(history, outbound)
}

func formatCatalogListBody(catalog []dbCatalogItem, maxFeatured int) string {
	return bf.FormatCatalogListBody(catalog, maxFeatured)
}

func bracketPackCount(name string) int { return bf.BracketPackCount(name) }

func buildPricingClarificationReply(formal bool, it *dbCatalogItem) string {
	return bf.BuildPricingClarificationReply(formal, it)
}

func boxerHistory() []dbMessage { return bf.BoxerHistory() }
