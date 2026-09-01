package buyerflow

import (
	"strings"
	"unicode/utf8"
)

// simpleFAQKeywords — Haiku-eligible intents (FAQ, catalog, price, stock).
var simpleFAQKeywords = []string{
	"harga", "price", "berapa", "rp", "idr",
	"stok", "stock", "ready", "tersedia", "ada", "punya", "jual",
	"alamat", "address", "lokasi", "dimana", "where",
	"jam buka", "jam operasional", "buka", "tutup", "hours", "open",
	"ongkir", "kirim", "pengiriman", "delivery",
	"ukuran", "size", "warna",
	"produk", "katalog", "koleksi", "jenis", "macam", "varian", "list",
	"jeans", "celana", "baju", "kulot", "apparel", "fashion",
	"mau", "tanya", "nanya", "boleh", "info",
}

// complexKeywords — Sonnet-eligible (complaints, negotiation, persuasion).
var complexKeywords = []string{
	"komplain", "complaint", "kecewa", "marah", "refund", "retur",
	"nego", "negotiasi", "diskon besar", "turunin harga",
	"banding", "ganti rugi", "tipu", "penipuan",
	"kenapa tidak", "kok belum", "sampai kapan",
}

const faqDirectAnswerMinScore = 0.72

// ClassifyComplexity decides simple (Haiku) vs complex (Sonnet) for hybrid routing.
// strongFAQMatch should come from retrieval.FAQDirectOK (RRF scale), not lexical kbTopScore.
func ClassifyComplexity(userText string, classifierLabel string, kbTopScore float64, strongFAQMatch bool) MessageComplexity {
	text := strings.ToLower(strings.TrimSpace(userText))
	runes := utf8.RuneCountInString(userText)

	if classifierLabel == "order_intent" || classifierLabel == "sensitive_escalate" {
		return ComplexityComplex
	}

	for _, kw := range complexKeywords {
		if strings.Contains(text, kw) {
			return ComplexityComplex
		}
	}

	// Strong FAQ match + short question → simple (often answered without Sonnet).
	if strongFAQMatch && runes <= 160 {
		return ComplexitySimple
	}
	// Legacy lexical score path (preload / disabled retrieval).
	if kbTopScore >= faqDirectAnswerMinScore && runes <= 160 {
		return ComplexitySimple
	}

	simpleHits := 0
	for _, kw := range simpleFAQKeywords {
		if strings.Contains(text, kw) {
			simpleHits++
		}
	}
	if simpleHits >= 1 && runes <= 200 {
		return ComplexitySimple
	}

	// Long multi-topic messages may need Sonnet synthesis; typical short WA chats → Haiku.
	if runes > 320 {
		return ComplexityComplex
	}
	return ComplexitySimple
}

// FAQDirectGuardsPass returns false when FAQ direct must be skipped (order/catalog intents).
// Shipping FAQ (ongkir, estimasi kirim) is allowed — those should use KB faq_direct.
func FAQDirectGuardsPass(query string) bool {
	if IsThirdPartyBuyerLookup(query) || IsSelfBuyerOrderLookup(query) || IsOrderStatusInquiry(query) {
		return false
	}
	if isShippingFAQAllowed(query) {
		return true
	}
	if IsCatalogListQuestion(query) || IsCatalogBrowsingIntent(query) ||
		isGeneralStoreCatalogQuestion(query) || IsRecommendationRequest(query) {
		return false
	}
	if IsCatalogProductInquiry(query) || IsProductSellInquiry(query, nil) ||
		looksLikeNamedProductSellInquiry(query) {
		return false
	}
	if IsConsultingPurchaseQuestion(query, nil) || IsPricingUnitClarification(query) ||
		brandVariantFAQGuard(query) {
		return false
	}
	return true
}

// looksLikeNamedProductSellInquiry — "jual abon sapi?" tanpa catalog match tetap ke catalog_db.
func looksLikeNamedProductSellInquiry(query string) bool {
	if isGeneralStoreCatalogQuestion(query) || HasPurchaseIntent(query) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(query))
	if text == "" || !IsQuestionLike(query) {
		return false
	}
	hasSell := strings.Contains(text, "jual") || strings.Contains(text, "jualan") || strings.Contains(text, "menjual")
	return hasSell
}

func brandVariantFAQGuard(query string) bool {
	if isGeneralStoreCatalogQuestion(query) || IsCatalogListQuestion(query) {
		return false
	}
	return hasBrandVariantSignal(strings.ToLower(strings.TrimSpace(query)))
}

func isShippingFAQAllowed(query string) bool {
	text := strings.ToLower(strings.TrimSpace(query))
	if text == "" {
		return false
	}
	if IsShippingQuoteQuestion(query) {
		return true
	}
	signals := []string{
		"pengiriman", "ongkir", "ongkos kirim", "kirim ke",
		"estimasi sampai", "berapa lama", "lama sampai", "waktu kirim",
		"luar kota", "wilayah pengiriman", "area kirim", "delivery",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// topKBMatchScore returns the best hybrid KB overlap score for the query.
func topKBMatchScore(query string, kb []KBEntry) float64 {
	if len(kb) == 0 {
		return 0
	}
	qTokens := tokenize(query)
	qScope := ExtractScopeKeywords(query)
	var best float64
	for _, entry := range kb {
		text := entry.Question + " " + entry.Answer
		eTokens := tokenize(text)
		lexical := overlapScore(qTokens, eTokens)
		semantic := overlapScore(qScope, ExtractScopeKeywords(text))
		rerank := overlapScore(tokenize(entry.Question), qTokens)
		score := lexical*0.5 + semantic*0.3 + rerank*0.2
		if score > best {
			best = score
		}
	}
	return best
}

// tryFAQDirectAnswer returns a KB answer without calling the LLM (cost optimization).
func tryFAQDirectAnswer(query string, kb []KBEntry) (answer string, ok bool) {
	if !FAQDirectGuardsPass(query) {
		return "", false
	}
	if len(kb) == 0 {
		return "", false
	}
	qTokens := tokenize(query)
	qScope := ExtractScopeKeywords(query)

	var best KBEntry
	var bestScore float64
	for _, entry := range kb {
		text := entry.Question + " " + entry.Answer
		eTokens := tokenize(text)
		lexical := overlapScore(qTokens, eTokens)
		semantic := overlapScore(qScope, ExtractScopeKeywords(text))
		rerank := overlapScore(tokenize(entry.Question), qTokens)
		score := lexical*0.5 + semantic*0.3 + rerank*0.2
		if score > bestScore {
			bestScore = score
			best = entry
		}
	}
	if bestScore < faqDirectAnswerMinScore || strings.TrimSpace(best.Answer) == "" {
		return "", false
	}
	return strings.TrimSpace(best.Answer), true
}
