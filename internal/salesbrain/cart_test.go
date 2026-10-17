package salesbrain

import (
	"strings"
	"testing"
)

var steakFacts = []CatalogFact{
	{No: 1, ID: "beef-500", Name: "Australian Beef 500 Gram", Price: 450000},
	{No: 2, ID: "tbone-1500", Name: "Australian Beef T-Bone 1.5 Kg", Price: 1750000},
	{No: 3, ID: "salmon-100", Name: "Salmon Steak 100 Gram", Price: 75000},
	{No: 4, ID: "salmon-250", Name: "Salmon Steak 250 Gram", Price: 105000},
	{No: 5, ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Price: 350000},
}

func TestApplyItemOps(t *testing.T) {
	cart, changed, unknown := ApplyItemOps(nil, []ItemOp{{No: 5, Qty: 1}, {No: 3, Qty: 2}, {No: 9, Qty: 1}}, steakFacts)
	if !changed || len(cart) != 2 || cart[0].ID != "tuna-100" || cart[1].Qty != 2 || len(unknown) != 1 || unknown[0] != 9 {
		t.Fatalf("got cart=%+v changed=%v unknown=%v", cart, changed, unknown)
	}
	if CartSubtotal(cart) != 350000+150000 {
		t.Fatalf("subtotal %d", CartSubtotal(cart))
	}
	cart, changed, _ = ApplyItemOps(cart, []ItemOp{{No: 3, Qty: 0}}, steakFacts)
	if !changed || len(cart) != 1 || cart[0].ID != "tuna-100" {
		t.Fatalf("qty 0 must remove the line, got %+v", cart)
	}
	_, changed, _ = ApplyItemOps(cart, []ItemOp{{No: 5, Qty: 1}}, steakFacts)
	if changed {
		t.Fatal("same qty must not count as a change")
	}
}

func TestRenderCartSummary(t *testing.T) {
	cart := []OrderLine{{ID: "salmon-100", Name: "Salmon Steak 100 Gram", Qty: 1, UnitPrice: 75000}, {ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000}}
	en := RenderCartSummary(cart, LangEN)
	for _, want := range []string{"Order summary", "1x Salmon Steak 100 Gram", "Rp75.000", "Subtotal: Rp425.000", "shipping not included"} {
		if !strings.Contains(en, want) {
			t.Errorf("EN summary missing %q:\n%s", want, en)
		}
	}
	id := RenderCartSummary(cart, LangID)
	for _, want := range []string{"Ringkasan pesanan", "Subtotal: Rp425.000", "belum termasuk ongkir"} {
		if !strings.Contains(id, want) {
			t.Errorf("ID summary missing %q:\n%s", want, id)
		}
	}
	if RenderCartSummary(nil, LangEN) != "" {
		t.Error("empty cart renders nothing")
	}
}

func TestResumeLineFor(t *testing.T) {
	if ResumeLineFor("address", LangEN) != T(TplResumeAddress, LangEN) || ResumeLineFor("recipient", LangID) != T(TplResumeRecipient, LangID) || ResumeLineFor("", LangEN) != "" {
		t.Fatal("resume line mapping wrong")
	}
}

func TestApplyItemOpsMatchesLegacyLinesByName(t *testing.T) {
	// Rows saved before cart lines carried an id are matched by name (case-insensitive).
	legacy := []OrderLine{{Name: "salmon steak 100 gram", Qty: 1, UnitPrice: 70000}}
	cart, changed, unknown := ApplyItemOps(legacy, []ItemOp{{No: 3, Qty: 2}}, steakFacts)
	if !changed || len(unknown) != 0 || len(cart) != 1 || cart[0].Qty != 2 || cart[0].UnitPrice != 75000 {
		t.Fatalf("legacy line must be updated in place at the current price, got %+v changed=%v", cart, changed)
	}
}

func TestApplyItemOpsDoesNotMutateInput(t *testing.T) {
	in := []OrderLine{
		{ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000},
		{ID: "salmon-100", Name: "Salmon Steak 100 Gram", Qty: 1, UnitPrice: 75000},
	}
	out, changed, _ := ApplyItemOps(in, []ItemOp{{No: 5, Qty: 3}, {No: 3, Qty: 0}}, steakFacts)
	if !changed || len(out) != 1 || out[0].Qty != 3 {
		t.Fatalf("got %+v changed=%v", out, changed)
	}
	if len(in) != 2 || in[0].Qty != 1 || in[1].ID != "salmon-100" {
		t.Fatalf("input cart was mutated: %+v", in)
	}
}

func TestRenderCartSummaryShowsLineTotals(t *testing.T) {
	cart := []OrderLine{{ID: "salmon-100", Name: "Salmon Steak 100 Gram", Qty: 2, UnitPrice: 75000}}
	want := "Order summary:\n1. 2x Salmon Steak 100 Gram — Rp150.000\nSubtotal: Rp150.000 (shipping not included)"
	if got := RenderCartSummary(cart, LangEN); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestResumeLineForConfirmAndUnknown(t *testing.T) {
	if ResumeLineFor("confirm", LangEN) != T(TplResumeConfirm, LangEN) || ResumeLineFor("nonsense", LangID) != "" {
		t.Fatal("confirm must map to its template and unknown fields to \"\"")
	}
}
