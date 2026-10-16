package buyerflow

import (
	"strings"
	"testing"
)

func cartPrimaryQty(st *OrderState) int {
	if st == nil {
		return 0
	}
	if len(st.Items) > 0 {
		return st.Items[0].Qty
	}
	return st.Qty
}

func cartPrimaryID(st *OrderState) string {
	if st == nil {
		return ""
	}
	if st.CatalogItemID != "" {
		return st.CatalogItemID
	}
	if len(st.Items) > 0 {
		return st.Items[0].CatalogItemID
	}
	return ""
}

func TestSteakOrderScriptEnglish_tunaNotSalmon(t *testing.T) {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	script := []string{
		"Hello i want to ask about your tuna steak product",
		"Ohh how much is the delivery fee? And can you delivery the steak today?",
		"Okay, i would like to order 1 tuna steak please",
	}
	var outcomes []TurnOutcome
	for _, msg := range script {
		outcomes = append(outcomes, s.Turn(msg))
	}
	assertSteakScriptRepliesNonEmpty(t, outcomes)
	if id := cartPrimaryID(s.Order); id != "tuna-100" {
		t.Fatalf("after order turn cart=%s want tuna-100 order=%+v", id, s.Order)
	}
	out := s.Turn("I ask about the tuna steak not the salmon")
	outcomes = append(outcomes, out)
	if id := cartPrimaryID(out.Order); id != "tuna-100" {
		t.Fatalf("after correction cart=%s want tuna-100", id)
	}
	if qty := cartPrimaryQty(out.Order); qty != 1 {
		t.Fatalf("correction must keep qty 1, got %d", qty)
	}
	if out.Path != PathOrderFlow {
		t.Fatalf("correction path=%s", out.Path)
	}
	out = s.Turn("1 pc")
	outcomes = append(outcomes, out)
	assertSteakScriptRepliesNonEmpty(t, outcomes)
	if id := cartPrimaryID(out.Order); id != "tuna-100" {
		t.Fatalf("after qty cart=%s", id)
	}
}

func TestSteakOrderEnglishSummaryRecipientPrompt(t *testing.T) {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	s.Turn("Okay, i would like to order 1 tuna steak please")
	if s.Order == nil {
		t.Fatal("expected draft order")
	}
	last := s.History[len(s.History)-1].Body
	if strings.Contains(last, "Pelanggan dapat melakukan pemesanan") {
		t.Fatalf("Indonesian KB must not appear in English summary:\n%s", last)
	}
	low := strings.ToLower(last)
	if !strings.Contains(low, "recipient") && !strings.Contains(low, "phone") {
		t.Fatalf("expected English recipient prompt in:\n%s", last)
	}
}

func assertSteakScriptRepliesNonEmpty(t *testing.T, outcomes []TurnOutcome) {
	t.Helper()
	for i, out := range outcomes {
		if strings.TrimSpace(out.Reply) == "" && out.Path != PathOrderStatus && out.Path != PathChannelMeta {
			t.Fatalf("turn %d path=%s must not have empty reply", i+1, out.Path)
		}
	}
}

func TestSteakOrderEnglishUpsellLocalized(t *testing.T) {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	out := s.Turn("Okay, i would like to order 1 tuna steak please")
	if strings.Contains(out.Reply, "Rekomendasi") || strings.Contains(out.Reply, "tidak wajib") {
		t.Fatalf("English order reply must not contain Indonesian upsell header:\n%s", out.Reply)
	}
	if strings.Contains(out.Reply, "You might also like") || strings.Contains(out.Reply, "Order Summary") {
		if strings.Contains(out.Reply, "Rekomendasi") {
			t.Fatalf("mixed-language upsell in:\n%s", out.Reply)
		}
	}
	pop := PushSavedBuyerLang(LangEN)
	defer pop()
	block := formatUpsellBlock(*s.Order, s.Catalog)
	if block != "" && !strings.Contains(block, "You might also like") {
		t.Fatalf("expected English upsell header, got:\n%s", block)
	}
}

func TestSteakOrderScriptIndonesian_cancelAbort(t *testing.T) {
	s := newOmahSteakSimulator()
	var outcomes []TurnOutcome
	outcomes = append(outcomes, s.Turn("disini jual apa saja ya ?"))
	outcomes = append(outcomes, s.Turn("saya mau yellow tail tuna 1 dong"))
	if s.Order == nil {
		t.Fatal("expected draft after tuna order")
	}
	for _, msg := range []string{"never mind", "reset"} {
		out := s.Turn(msg)
		outcomes = append(outcomes, out)
		if out.Path != PathOrderCancel {
			t.Fatalf("%q path=%s want order_cancel", msg, out.Path)
		}
		if out.Order != nil {
			t.Fatalf("%q should clear draft", msg)
		}
		if strings.TrimSpace(out.Reply) == "" {
			t.Fatalf("%q must not return empty reply", msg)
		}
	}
	assertSteakScriptRepliesNonEmpty(t, outcomes)
	s.Turn("saya mau yellow tail tuna 1 dong")
	out := s.Turn("batalkan pesanan")
	if out.Path != PathOrderCancel || out.Order != nil {
		t.Fatalf("batalkan pesanan path=%s order=%v", out.Path, out.Order)
	}
}

func TestSteakOrderPronounBuyFromOutboundEnglish(t *testing.T) {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	s.History = append(s.History, Message{
		Direction: "out",
		Body:      "We currently have the Yellow Tail Tuna Portion (100 Gram) available for Rp350,000/pcs. Let us know if you'd like to order.",
	})
	out := s.Turn("okay i want buy it")
	if out.Path != PathOrderFlow {
		t.Fatalf("path=%s want order_flow reply=%q", out.Path, out.Reply)
	}
	if id := cartPrimaryID(out.Order); id != "tuna-100" {
		t.Fatalf("cart=%s want tuna-100", id)
	}
	low := strings.ToLower(out.Reply)
	if !strings.Contains(low, "recipient") && !strings.Contains(low, "phone") {
		t.Fatalf("expected English order summary/recipient prompt: %q", out.Reply)
	}
}

func TestSteakOrderResetNotInProductName(t *testing.T) {
	catalog := omahSteakCatalog()
	// Whole-message abort only — product names containing "reset" must not cancel.
	if IsOrderCancelRequest("Australian Beef preset cut 1 pcs") {
		t.Fatal("preset in product name must not trigger cancel")
	}
	_ = catalog
	if !IsOrderCancelRequest("reset") {
		t.Fatal("bare reset should cancel draft")
	}
}
