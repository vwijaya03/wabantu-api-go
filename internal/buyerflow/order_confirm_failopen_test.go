package buyerflow

import (
	"strings"
	"testing"
)

// The LLM-first pipeline keeps a "confirm" step in the shared Redis state. When a turn falls back
// to the legacy flow the FSM must read it as a finished address step, never get stuck on RetryStep.

func confirmStepState(complete bool) OrderState {
	st := OrderState{
		Step: "confirm", CatalogItemID: "abon-500", ProductName: "Abon Sapi 500 Gram", Qty: 1, UnitPrice: 25000, SellUnit: "pcs",
		Items:         []OrderLineState{{CatalogItemID: "abon-500", ProductName: "Abon Sapi 500 Gram", Qty: 1, UnitPrice: 25000, SellUnit: "pcs"}},
		RecipientName: "Ana", RecipientPhone: "+62872839472828",
	}
	if complete {
		st.Street, st.City, st.Province, st.PostalCode = "Jl. Melati No 5", "Surabaya", "Jawa Timur", "60177"
	}
	return st
}

func TestNormalizeOrderStateReadsConfirmAsAddressStep(t *testing.T) {
	st := normalizeOrderState(confirmStepState(true))
	if st.Step != "ask_address_full" {
		t.Fatalf("confirm must read as ask_address_full, got %q", st.Step)
	}
}

func TestLegacyFlowFinishesOrderFromConfirmStepWithCompleteShipping(t *testing.T) {
	st := confirmStepState(true)
	persisted := 0
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "ok", State: &st, Catalog: omahCadburyLiveCatalog(), Profile: foodProfile(),
	}, func(OrderState) (string, error) {
		persisted++
		return "9f1c2d3e-0000-0000-0000-000000000000", nil
	})
	if !res.Completed || !res.Cleared || persisted != 1 {
		t.Fatalf("a confirm state with complete shipping must finish the order: completed=%v cleared=%v persisted=%d reply=%q",
			res.Completed, res.Cleared, persisted, res.Reply)
	}
	if strings.TrimSpace(res.Reply) == "" {
		t.Fatal("the buyer must get the order confirmation")
	}
}

func TestLegacyFlowAsksAddressFromConfirmStepWithIncompleteShipping(t *testing.T) {
	st := confirmStepState(false)
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "ok", State: &st, Catalog: omahCadburyLiveCatalog(), Profile: foodProfile(),
	}, func(OrderState) (string, error) {
		t.Fatal("must not persist without a complete address")
		return "", nil
	})
	if res.Completed || res.State == nil || res.State.Step != "ask_address_full" {
		t.Fatalf("incomplete shipping must stay on ask_address_full: %+v", res)
	}
	if strings.Contains(res.Reply, "data ordernya") {
		t.Fatal("must not answer with the generic retry text")
	}
}
