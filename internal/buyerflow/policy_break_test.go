package buyerflow

import (
	"strings"
	"testing"
)

func TestPolicyQuestionMustNotBreakActiveCheckout(t *testing.T) {
	catalog := omahCatalog()
	for _, msg := range []string{
		"masih mau order item yang lain?",
		"mau nambah pesanan dong",
		"bisa tambah lagi ga?",
	} {
		if ShouldBreakOrderFlow(msg, "ask_recipient", catalog) {
			t.Fatalf("must not break checkout: %q", msg)
		}
		if IsConsultingPurchaseQuestion(msg, catalog) {
			t.Fatalf("must not be consulting purchase during checkout: %q", msg)
		}
	}

	sim := NewOmahSimulator()
	sim.Turn("mau beli abon sapi 2 pcs")
	if sim.Order == nil {
		t.Fatal("setup failed")
	}
	out := sim.Turn("masih mau order item yang lain?")
	if out.BrokeFlow || sim.Order == nil {
		t.Fatalf("cart cleared on policy question: broke=%v order=%v reply=%q", out.BrokeFlow, sim.Order, out.Reply)
	}
	if out.Path != PathOrderFlow {
		t.Fatalf("want order_flow path, got %q", out.Path)
	}
	if !strings.Contains(strings.ToLower(out.Reply), "boleh tambah") {
		t.Fatalf("expected CS-style add-more reply: %q", out.Reply)
	}
}

func TestAppendAcknowledgementCSStyle(t *testing.T) {
	catalog := omahCatalog()
	st := OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "abon-500g",
		ProductName:   "Abon Sapi 500G",
		Qty:           2,
		UnitPrice:     35000,
		SellUnit:      "pcs",
	}
	tmpl := orderTemplatesFromKB(nil, false)
	_, reply := TryAppendItemsDuringCheckout(&st, "cadbury mini 1 pcs", catalog, tmpl, false, nil)
	if !strings.Contains(reply, "ditambahkan") {
		t.Fatalf("expected append acknowledgement in reply: %q", reply)
	}
}
