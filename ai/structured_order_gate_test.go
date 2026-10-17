package ai

import (
	"strings"
	"testing"

	"encore.app/wabantu/internal/salesbrain"
)

const incidentOrderList = "I would like to order\n1 of Australian beef 500grams\n1 of salmon steak 100gr\n1 of yellow tail tuna"

// A pasted multi-line order is only a "break" of a checkout that exists. With no cart it is a new
// order and must reach the order flow (it used to be skipped, so the list never became an order).
func TestStructuredListBreaksCheckoutOnlyWithCart(t *testing.T) {
	if !isStructuredOrderCheckoutBreak(incidentOrderList) {
		t.Fatal("fixture: the incident list is a text-level checkout break")
	}
	if structuredListBreaksCheckout(incidentOrderList, nil) {
		t.Fatal("no cart: the list must not break anything, it has to reach the order flow")
	}
	if structuredListBreaksCheckout(incidentOrderList, &orderState{Step: "ask_product"}) {
		t.Fatal("a state without any product is not an active checkout")
	}

	multi := &orderState{Step: "ask_recipient", Items: []orderLineState{{CatalogItemID: "beef-500", ProductName: "Australian Beef 500 Gram", Qty: 1}}}
	single := &orderState{Step: "ask_recipient", ProductName: "Salmon Steak 100 Gram", Qty: 1}
	for name, st := range map[string]*orderState{"multi-item cart": multi, "single-product cart": single} {
		if !structuredListBreaksCheckout(incidentOrderList, st) {
			t.Errorf("%s: an active checkout must still break as before", name)
		}
	}
	if structuredListBreaksCheckout("berapa harga tuna?", multi) {
		t.Fatal("text that is not a structured list never breaks a checkout")
	}
}

// noStockGuard stands in for guardStructuredOrderStock when stock is not what a test is about.
func noStockGuard(st orderState) (orderState, string, bool) { return st, "", false }

func lineOf(id, name string, qty int, price float64) orderLineState {
	return orderLineState{CatalogItemID: id, ProductName: name, Qty: qty, UnitPrice: price}
}

// A list pasted while a cart exists joins it: the old line stays, the new lines are added (same
// product adds quantity), and recipient and address already given are kept.
func TestMergeStructuredIntoCartKeepsCartAndRecipient(t *testing.T) {
	existing := orderState{
		Step: "ask_address_full", PersistedOrderID: "ord-1",
		RecipientName: "Ana", RecipientPhone: "0872839472828",
		Items: []orderLineState{lineOf("beef-500", "Australian Beef 500 Gram", 1, 450000)},
	}
	added := []orderLineState{
		lineOf("salmon-100", "Salmon Steak 100 Gram", 1, 75000),
		lineOf("tuna-100", "Yellow Tail Tuna Portion 100 Gram", 1, 350000),
		lineOf("beef-500", "Australian Beef 500 Gram", 2, 450000), // same product: quantity adds up
	}
	out := mergeStructuredIntoCart(existing, added, noStockGuard)
	if out.Blocked || out.NeedVariant {
		t.Fatalf("unexpected block/variant: %+v", out)
	}
	if len(out.State.Items) != 3 {
		t.Fatalf("want 3 distinct lines, got %+v", out.State.Items)
	}
	if out.State.Items[0].CatalogItemID != "beef-500" || out.State.Items[0].Qty != 3 {
		t.Fatalf("beef must keep its place and add quantity (1+2): %+v", out.State.Items[0])
	}
	if out.State.RecipientName != "Ana" || out.State.RecipientPhone != "0872839472828" || out.State.PersistedOrderID != "ord-1" {
		t.Fatalf("recipient and draft binding must be kept: %+v", out.State)
	}
	if out.State.Step != "ask_address_full" {
		t.Fatalf("recipient is known, the address is next: step = %q", out.State.Step)
	}
	if existing.Items[0].Qty != 1 || len(existing.Items) != 1 {
		t.Fatal("the existing state value must not be mutated")
	}
}

// A single-product cart keeps that product in the flat fields only; merging must not lose it.
func TestMergeStructuredIntoCartFromFlatSingleItem(t *testing.T) {
	existing := orderState{Step: "ask_recipient", CatalogItemID: "tuna-100", ProductName: "Yellow Tail Tuna Portion 100 Gram", Qty: 2, UnitPrice: 350000}
	added := []orderLineState{lineOf("salmon-100", "Salmon Steak 100 Gram", 1, 75000)}
	out := mergeStructuredIntoCart(existing, added, noStockGuard)
	if len(out.State.Items) != 2 || out.State.Items[0].CatalogItemID != "tuna-100" || out.State.Items[0].Qty != 2 {
		t.Fatalf("the flat tuna line (qty 2) must lead the merged cart: %+v", out.State.Items)
	}
	if out.State.Step != "ask_recipient" {
		t.Fatalf("no recipient yet: step = %q", out.State.Step)
	}

	// A product still waiting for its quantity is not a cart line: the pasted list replaces it.
	pending := orderState{Step: "ask_qty", CatalogItemID: "salmon-100", ProductName: "Salmon Steak 100 Gram"}
	out = mergeStructuredIntoCart(pending, []orderLineState{lineOf("tuna-100", "Yellow Tail Tuna Portion 100 Gram", 1, 350000)}, noStockGuard)
	if len(out.State.Items) != 1 || out.State.Items[0].CatalogItemID != "tuna-100" {
		t.Fatalf("a quantity-less selection must not become a phantom line: %+v", out.State.Items)
	}
}

// The stock guard must see the merged cart: 2 tuna already in the cart plus 3 pasted is 5.
func TestMergeStructuredIntoCartStockGuardCoversMergedLines(t *testing.T) {
	var guarded orderState
	guard := func(st orderState) (orderState, string, bool) {
		guarded = st
		return st, "Baris 1: stok tidak cukup", true
	}
	existing := orderState{Step: "ask_recipient", Items: []orderLineState{lineOf("tuna-100", "Yellow Tail Tuna Portion 100 Gram", 2, 350000)}}
	out := mergeStructuredIntoCart(existing, []orderLineState{lineOf("tuna-100", "Yellow Tail Tuna Portion 100 Gram", 3, 350000)}, guard)
	if !out.Blocked || strings.TrimSpace(out.BlockReply) == "" {
		t.Fatalf("a blocking guard must block the merge: %+v", out)
	}
	if len(guarded.Items) != 1 || guarded.Items[0].Qty != 5 {
		t.Fatalf("the guard must receive the merged cart (tuna 2+3=5), got %+v", guarded.Items)
	}
}

func TestNextCheckoutPromptAsksOnlyForWhatIsMissing(t *testing.T) {
	tmpl := orderFlowTemplates{AskVariant: "ASK_VARIANT", AskRecipient: "ASK_RECIPIENT", AskAddressFull: "ASK_ADDRESS", ClarifyAddress: "CLARIFY_ADDRESS"}
	ready := []orderLineState{lineOf("tuna-100", "Yellow Tail Tuna Portion 100 Gram", 1, 350000)}

	cases := []struct {
		name string
		st   orderState
		want string
	}{
		{"no recipient", orderState{Items: ready}, "ASK_RECIPIENT"},
		{"recipient without phone", orderState{Items: ready, RecipientName: "Ana"}, "ASK_RECIPIENT"},
		{"recipient known, no address", orderState{Items: ready, RecipientName: "Ana", RecipientPhone: "0872839472828"}, "ASK_ADDRESS"},
		{"partial address", orderState{Items: ready, RecipientName: "Ana", RecipientPhone: "0872839472828", Street: "Jl. Perak 1"}, "CLARIFY_ADDRESS"},
		{"line without quantity", orderState{Items: []orderLineState{{CatalogItemID: "x", ProductName: "X"}}}, "ASK_VARIANT"},
	}
	for _, c := range cases {
		if got := nextCheckoutPrompt(c.st, tmpl, salesbrain.LangEN); got != c.want {
			t.Errorf("%s: prompt = %q, want %q", c.name, got, c.want)
		}
	}

	complete := orderState{
		Items: ready, RecipientName: "Ana", RecipientPhone: "0872839472828",
		Street: "Jl. Perak 1", City: "Surabaya", Province: "Jawa Timur", PostalCode: "60177",
	}
	if !complete.ShippingComplete() {
		t.Fatal("fixture: shipping data should be complete")
	}
	for _, lang := range []salesbrain.Lang{salesbrain.LangEN, salesbrain.LangID} {
		if got, want := nextCheckoutPrompt(complete, tmpl, lang), salesbrain.T(salesbrain.TplResumeConfirm, lang); got != want {
			t.Errorf("complete cart (%s): prompt = %q, want the %s confirm template %q", lang, got, lang, want)
		}
	}
}
