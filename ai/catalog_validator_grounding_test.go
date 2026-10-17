package ai

import (
	"strings"
	"testing"

	"encore.app/wabantu/internal/salesbrain"
)

// omahGroundingCatalog is the incident tenant's steak catalog (same products and prices as
// internal/buyerflow/fixtures_omah_steak.go).
func omahGroundingCatalog() []dbCatalogItem {
	return []dbCatalogItem{
		{ID: "beef-500", ExternalCode: "AUSTRALIAN_BEEF_500_GRAM", Name: "Australian Beef 500 Gram", SellPrice: 450000, SellUnit: "pcs"},
		{ID: "tbone-1500", ExternalCode: "AUSTRALIAN_BEEF_T_BONE_1_5_KG", Name: "Australian Beef T-Bone 1.5 Kg", SellPrice: 1750000, SellUnit: "pcs"},
		{ID: "salmon-100", ExternalCode: "SALMON_STEAK_100_GRAM", Name: "Salmon Steak 100 Gram", SellPrice: 75000},
		{ID: "salmon-250", ExternalCode: "SALMON_STEAK_250_GRAM_SALINAN_CZED", Name: "Salmon Steak 250 Gram", SellPrice: 105000},
		{ID: "tuna-100", ExternalCode: "YELLOW_TAIL_TUNA_PORTION_100_GRAM", Name: "Yellow Tail Tuna Portion 100 Gram", SellPrice: 350000, SellUnit: "pcs"},
	}
}

func omahGroundingProfile() *dbBusinessProfile {
	return &dbBusinessProfile{BusinessName: "Omah Apparel", Tone: strPtr("friendly")}
}

// omahGroundingCart is the cart of the incident chat: tuna 350000 + salmon 75000 + beef 450000.
func omahGroundingCart() *orderState {
	return &orderState{Step: "ask_recipient", Items: []orderLineState{
		{CatalogItemID: "tuna-100", ProductName: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000},
		{CatalogItemID: "salmon-100", ProductName: "Salmon Steak 100 Gram", Qty: 1, UnitPrice: 75000},
		{CatalogItemID: "beef-500", ProductName: "Australian Beef 500 Gram", Qty: 1, UnitPrice: 450000},
	}}
}

// noCatalogReply makes groundLLMReply skip the catalog_db substitution so the tests reach
// the final fallback deterministically.
func noCatalogReply(string, *dbBusinessProfile, []dbCatalogItem, []dbMessage) (string, bool) {
	return "", false
}

func omahPaymentKB() []dbKBEntry {
	return []dbKBEntry{{
		ID: "kb-pay-method", Question: "Apa saja metode pembayaran yang tersedia?",
		Answer:   "Saat ini kami menerima pembayaran melalui Transfer Bank dan QRIS.",
		Category: strPtr("pembayaran"), IsActive: true,
	}}
}

// The cart total is a fact the buyer was shown; replacing a reply that repeats it was the
// incident's "8 of 24 LLM replies replaced by canned Indonesian".
func TestGroundingAcceptsCartTotal(t *testing.T) {
	reply := "Your order so far is the tuna, the salmon steak and the beef. Total: Rp875.000 (shipping not included)."
	final, grounded, reason := groundLLMReply(reply, "what is my total?", salesbrain.LangEN,
		omahGroundingProfile(), omahGroundingCatalog(), nil, omahGroundingCart(), nil, noCatalogReply)
	if grounded {
		t.Fatalf("cart total Rp875.000 must not be replaced; got %q (reason %q)", final, reason)
	}
	if final != reply {
		t.Fatalf("reply must pass through untouched, got %q", final)
	}
}

func TestGroundingAcceptsQtyTimesPrice(t *testing.T) {
	reply := "2x Salmon Steak 100 Gram would be Rp150.000."
	final, grounded, reason := groundLLMReply(reply, "how much for two salmon steaks?", salesbrain.LangEN,
		omahGroundingProfile(), omahGroundingCatalog(), nil, nil, nil, noCatalogReply)
	if grounded {
		t.Fatalf("qty x price Rp150.000 must not be replaced; got %q (reason %q)", final, reason)
	}
}

func TestGroundingAcceptsAmountFromKB(t *testing.T) {
	kb := []dbKBEntry{{ID: "kb-min", Question: "Minimum order?", Answer: "Minimum belanja Rp100.000 untuk gratis packing.", Category: strPtr("order"), IsActive: true}}
	reply := "The minimum for free packing is Rp100.000."
	if _, grounded, reason := groundLLMReply(reply, "is there a minimum order?", salesbrain.LangEN,
		omahGroundingProfile(), omahGroundingCatalog(), kb, nil, nil, noCatalogReply); grounded {
		t.Fatalf("an amount quoted from the KB must not be replaced (reason %q)", reason)
	}
}

func TestGroundingReplacesInventedAmountWithCheckWithTeam(t *testing.T) {
	cases := []struct {
		name  string
		lang  salesbrain.Lang
		reply string
		ask   string
	}{
		{"english", salesbrain.LangEN, "The yellow tail tuna is only Rp325.000 today.", "how much is the tuna?"},
		{"indonesian", salesbrain.LangID, "Yellow tail tuna hari ini cuma Rp325.000 ya kak.", "tuna berapa harganya?"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			final, grounded, reason := groundLLMReply(c.reply, c.ask, c.lang,
				omahGroundingProfile(), omahGroundingCatalog(), nil, nil, nil, noCatalogReply)
			if !grounded {
				t.Fatalf("invented Rp325.000 must be grounded; got %q", final)
			}
			if reason == "" {
				t.Fatal("a grounded reply must carry a reason")
			}
			if want := salesbrain.T(salesbrain.TplCheckWithTeam, c.lang); final != want {
				t.Fatalf("final = %q, want the %s check-with-team template %q", final, c.lang, want)
			}
			if strings.Contains(final, "325") || strings.Contains(final, "Saya belum menemukan") {
				t.Fatalf("invented price or canned Indonesian leaked: %q", final)
			}
		})
	}
}

// The old validator turned an LLM answer that merely echoed the tool's Indonesian not-found
// sentence into the same canned sentence for everyone; the English buyer now gets English.
func TestGroundingGenericNotFoundIsLocalized(t *testing.T) {
	final, grounded, reason := groundLLMReply("Saya belum menemukan data tersebut di katalog saat ini.",
		"do you sell ribeye?", salesbrain.LangEN,
		omahGroundingProfile(), omahGroundingCatalog(), nil, nil, nil, noCatalogReply)
	if !grounded || reason != "generic_catalog_not_found" {
		t.Fatalf("grounded=%v reason=%q", grounded, reason)
	}
	if want := salesbrain.T(salesbrain.TplCheckWithTeam, salesbrain.LangEN); final != want {
		t.Fatalf("final = %q, want %q", final, want)
	}
}

func TestGroundingEmptyLangFallsBackToIndonesian(t *testing.T) {
	final, grounded, _ := groundLLMReply("Tunanya Rp325.000 hari ini.", "tuna berapa?", "",
		omahGroundingProfile(), omahGroundingCatalog(), nil, nil, nil, noCatalogReply)
	if !grounded || final != salesbrain.T(salesbrain.TplCheckWithTeam, salesbrain.LangID) {
		t.Fatalf("grounded=%v final=%q", grounded, final)
	}
}

func TestGroundingPaymentFallbackUsesKBThenTemplate(t *testing.T) {
	invented := "Silakan transfer Rp325.000 ke rekening kami."

	final, grounded, reason := groundLLMReply(invented, "bisa minta nomor rekeningnya?", salesbrain.LangID,
		omahGroundingProfile(), omahGroundingCatalog(), omahPaymentKB(), nil, nil, noCatalogReply)
	if !grounded || reason == "" {
		t.Fatalf("grounded=%v reason=%q", grounded, reason)
	}
	if !strings.Contains(final, "Transfer Bank dan QRIS") {
		t.Fatalf("payment fallback must answer from the KB, got %q", final)
	}

	final, grounded, reason = groundLLMReply(invented, "bisa minta nomor rekeningnya?", salesbrain.LangEN,
		omahGroundingProfile(), omahGroundingCatalog(), nil, nil, nil, noCatalogReply)
	if !grounded || reason != "payment_no_kb" {
		t.Fatalf("grounded=%v reason=%q", grounded, reason)
	}
	if want := salesbrain.T(salesbrain.TplCheckWithTeam, salesbrain.LangEN); final != want {
		t.Fatalf("no KB payment answer: final = %q, want the EN template %q", final, want)
	}
	if strings.Contains(final, "rekening") {
		t.Fatalf("English buyer must not get the old Indonesian payment sentence: %q", final)
	}
}

func TestValidateReplyAgainstCatalogUsesFacts(t *testing.T) {
	catalog := omahGroundingCatalog()
	reply := "Total: Rp875.000"

	withCart := groundingFacts(catalog, nil, omahGroundingCart())
	if v := validateReplyAgainstCatalog(reply, catalog, withCart); !v.OK {
		t.Fatalf("cart total must validate against cart facts, got %s", v.Reason)
	}
	withoutCart := groundingFacts(catalog, nil, nil)
	if v := validateReplyAgainstCatalog(reply, catalog, withoutCart); v.OK {
		t.Fatal("the same total without a cart is an invented amount and must fail")
	}
	if v := validateReplyAgainstCatalog("Rp325.000", catalog, withCart); v.OK {
		t.Fatal("invented amount must still fail with facts")
	}
}

// A cart without Items keeps its one product in the flat fields; its price (not a catalog
// multiple here) and subtotal must still be allowed amounts.
func TestGroundingFactsCartFallsBackToSingleItem(t *testing.T) {
	single := &orderState{ProductName: "Custom Platter", Qty: 3, UnitPrice: 123000}
	f := groundingFacts(omahGroundingCatalog(), nil, single)
	if !f.Amounts[123000] || !f.Amounts[369000] {
		t.Fatalf("single-item cart price and subtotal (3 x 123000) must be allowed amounts: %v", f.Amounts[123000])
	}
	if empty := groundingFacts(omahGroundingCatalog(), nil, &orderState{}); empty.Amounts[0] {
		t.Fatal("an empty cart must add nothing")
	}
}
