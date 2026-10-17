package buyerflow

import (
	"strings"
	"testing"
)

func TestBrandVariantListEnglishWhenSavedEN(t *testing.T) {
	pop := PushSavedBuyerLang(LangEN)
	defer pop()
	reply := buildBrandVariantListReply(false, "maggi", maggiCatalog(), 10, "i wanna buy Maggi")
	if strings.Contains(reply, "ya kak") || strings.Contains(reply, "varian yang ada") {
		t.Fatalf("expected English variant list, got:\n%s", reply)
	}
	if !strings.Contains(reply, "variants we have") && !strings.Contains(reply, "available") {
		t.Fatalf("expected English header, got:\n%s", reply)
	}
}

func TestBrandVariantListIndonesianWhenSavedID(t *testing.T) {
	pop := PushSavedBuyerLang(LangID)
	defer pop()
	reply := buildBrandVariantListReply(false, "magi", maggiCatalog(), 10, "mau beli maggi")
	if !strings.Contains(reply, "ya kak") {
		t.Fatalf("expected Indonesian variant list, got:\n%s", reply)
	}
}

func TestCheckoutAckAndSummaryEnglishWhenSavedEN(t *testing.T) {
	pop := PushSavedBuyerLang(LangEN)
	defer pop()
	ack := checkoutItemAddedAck(false, "Maggi Kari 1 pcs")
	if strings.Contains(ack, "Siap kak") || strings.Contains(ack, "ditambahkan ke pesanan") {
		t.Fatalf("expected English ack, got %q", ack)
	}
	st := OrderState{
		ProductName: "Maggi Kari",
		Qty:         1,
		UnitPrice:   65000,
		SellUnit:    "pcs",
	}
	summary := formatOrderSummary(st)
	if strings.Contains(summary, "Ringkasan Pesanan") {
		t.Fatalf("expected English summary title, got:\n%s", summary)
	}
	if !strings.Contains(summary, "Order Summary") {
		t.Fatalf("missing Order Summary heading:\n%s", summary)
	}
}

func TestOrderSummaryIndonesianWhenSavedID(t *testing.T) {
	pop := PushSavedBuyerLang(LangID)
	defer pop()
	st := OrderState{
		ProductName: "Maggi Kari",
		Qty:         1,
		UnitPrice:   65000,
		SellUnit:    "pcs",
	}
	summary := formatOrderSummary(st)
	if !strings.Contains(summary, "Ringkasan Pesanan") {
		t.Fatalf("expected Indonesian summary, got:\n%s", summary)
	}
}

func TestLanguageMenuAnswerDoesNotFallThrough(t *testing.T) {
	sim := newOmahSimulator()
	out := sim.Turn("hi")
	if out.Path != PathBuyerLanguage {
		t.Fatalf("setup: expected language ask, got path=%s", out.Path)
	}
	// LanguagePreferenceAskReply prints "Indonesia / English (or 1 / 2)", so 1 = Indonesia.
	out = sim.Turn("1")
	if out.Path != PathBuyerLanguage {
		t.Fatalf("path=%s want %s (not handoff/catalog)", out.Path, PathBuyerLanguage)
	}
	if out.Path == PathHumanHandoff || out.Path == PathAiNoAnswer {
		t.Fatalf("language menu must not hand off: path=%s reply=%q", out.Path, out.Reply)
	}
	if sim.SavedBuyerLang != LangID {
		t.Fatalf("saved=%q want id", sim.SavedBuyerLang)
	}
	if !strings.Contains(out.Reply, "Bahasa Indonesia") {
		t.Fatalf("expected Indonesian confirm: %q", out.Reply)
	}
}

func TestExplicitSpeakEnglishConfirmOnly(t *testing.T) {
	sim := newOmahSimulator()
	out := sim.Turn("can i speak in english ?")
	if out.Path != PathBuyerLanguage {
		t.Fatalf("path=%s want buyer_language reply=%q", out.Path, out.Reply)
	}
	if sim.SavedBuyerLang != LangEN {
		t.Fatalf("saved=%q want en", sim.SavedBuyerLang)
	}
}
