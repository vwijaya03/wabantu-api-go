package buyerflow

import "strings"

// offTopicProductKeywords — products/services clearly outside typical apparel/retail WA tenants.
// If present in the customer message, the line must also mention the tenant catalog or it is rejected.
var offTopicProductKeywords = []string{
	"nasi", "goreng", "makanan", "menu", "sarapan", "makan", "minuman", "kopi", "teh",
	"ayam", "bebek", "ikan", "bakso", "mie", "pizza", "burger", "catering", "warteg",
	"pulsa", "token listrik", "listrik", "pln", "hotel", "tiket", "travel", "gojek",
	"grab food", "shopee food",
}

// mentionsOffTopicProduct returns true when the message names a product category outside the tenant catalog.
func mentionsOffTopicProduct(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	for _, kw := range offTopicProductKeywords {
		// Word-boundary match avoids false positives (e.g. "transfer" ⊃ "travel").
		if len(kw) >= 4 && strings.Contains(text, " "+kw+" ") {
			return true
		}
		if strings.HasPrefix(text, kw+" ") || strings.HasSuffix(text, " "+kw) || text == kw {
			return true
		}
	}
	return false
}

// messageReferencesBusinessCatalog is true when the message mentions tenant scope or apparel/fashion terms.
func messageReferencesBusinessCatalog(userText string, scopeKeywords []string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	for _, kw := range apparelProductKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	for _, kw := range scopeKeywords {
		if len(kw) >= 3 && strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// IsOffBusinessProductRequest rejects orders/questions about unrelated goods (e.g. nasi goreng at a jeans shop).
func IsOffBusinessProductRequest(userText string, scopeKeywords []string) bool {
	// "jual indomie goreng?" → catalog_db (not found), bukan out_of_scope.
	if looksLikeNamedProductSellInquiry(userText) || IsCatalogProductInquiry(userText) {
		return false
	}
	if !mentionsOffTopicProduct(userText) {
		return false
	}
	return !messageReferencesBusinessCatalog(userText, scopeKeywords)
}

// businessScopeKeywords builds scope tokens from the tenant business profile.
func businessScopeKeywords(profile *BusinessProfile) []string {
	if profile == nil {
		return nil
	}
	var parts []string
	parts = append(parts, profile.BusinessName)
	parts = append(parts, strOrEmpty(profile.Description))
	parts = append(parts, strOrEmpty(profile.ProductsServices))
	return ExtractScopeKeywords(strings.Join(parts, " "))
}
