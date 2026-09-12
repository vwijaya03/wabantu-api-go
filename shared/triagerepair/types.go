package triagerepair

import "strings"

const (
	OpDraftItemsReplace          = "draft_items_replace"
	OpDraftItemsMerge            = "draft_items_merge"
	OpCatalogFieldPatch          = "catalog_field_patch"
	OpKBAnswerPatch              = "kb_answer_patch"
	OpBusinessProfilePatch       = "business_profile_patch"
	OpOutboundCorrectionProposal = "outbound_correction_proposal"
)

var ValidOps = map[string]bool{
	OpDraftItemsReplace:          true,
	OpDraftItemsMerge:            true,
	OpCatalogFieldPatch:          true,
	OpKBAnswerPatch:              true,
	OpBusinessProfilePatch:       true,
	OpOutboundCorrectionProposal: true,
}

const (
	BlockPaymentInFlight    = "payment_in_flight"
	BlockIdentityMismatch   = "identity_anchor_mismatch"
	BlockCartNotPersisted   = "cart_not_persisted"
	BlockNeedsCustomerInput = "needs_customer_input"
	BlockNotDraftUnpaid     = "not_draft_unpaid"
	BlockStaleHash          = "stale_hash"
	BlockUnknownVariant     = "unknown_variant"
	BlockStockSensitive     = "stock_sensitive"
	BlockRedisCart          = "redis_cart"
	BlockSessionOrderState  = "session_order_state"
)

// AllowedCatalogFields is the closed set for catalog_field_patch.
var AllowedCatalogFields = map[string]bool{
	"name": true, "sell_price": true, "is_active": true, "is_storefront_visible": true,
}

// AllowedKBFields is the closed set for kb_answer_patch.
var AllowedKBFields = map[string]bool{
	"answer": true, "question": true, "is_active": true,
}

// AllowedProfileFields is the closed set for business_profile_patch.
var AllowedProfileFields = map[string]bool{
	"ai_greeting": true, "ai_tone": true, "ai_enabled": true,
}

func ValidOperation(op string) bool {
	return ValidOps[strings.TrimSpace(op)]
}
