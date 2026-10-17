package buyerflow

import (
	"errors"
	"strings"
	"testing"
)

// The LLM-first pipeline keeps a "confirm" step in the shared Redis state. When a turn reaches the
// legacy FSM while the state is at that step the FSM is fail-closed: it saves the order only after an
// explicit buyer approval, never from an arbitrary message.

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

func TestNormalizeOrderStateKeepsConfirmStep(t *testing.T) {
	st := normalizeOrderState(confirmStepState(true))
	if st.Step != "confirm" {
		t.Fatalf("confirm must stay confirm (never read as an address step), got %q", st.Step)
	}
}

func TestLegacyFlowDoesNotFinishOrderFromConfirmStepWithoutExplicitConfirm(t *testing.T) {
	for _, text := range []string{
		"hmm", "tolong kirim hari ini", "ok tapi ganti alamat", "oke berapa ongkirnya?", "tidak", "jangan dulu",
		"siap kak ditunggu kabarnya besok pagi ya", "terima kasih", "thanks",
	} {
		st := confirmStepState(true)
		persisted := 0
		res := AdvanceOrderFlow(OrderFlowInput{
			UserText: text, State: &st, Catalog: omahCadburyLiveCatalog(), Profile: foodProfile(),
		}, func(OrderState) (string, error) {
			persisted++
			return "9f1c2d3e-0000-0000-0000-000000000000", nil
		})
		if res.Completed || res.Cleared || persisted != 0 {
			t.Fatalf("%q must not finish the order: completed=%v cleared=%v persisted=%d", text, res.Completed, res.Cleared, persisted)
		}
		if res.State == nil || res.State.Step != "confirm" {
			t.Fatalf("%q must keep the draft at confirm: %+v", text, res.State)
		}
		if !strings.Contains(res.Reply, "OK") || !strings.Contains(res.Reply, "Abon Sapi 500 Gram") {
			t.Fatalf("%q must re-show the summary and ask for OK, got %q", text, res.Reply)
		}
		if strings.Contains(res.Reply, "data ordernya") {
			t.Fatalf("%q must not answer with the generic retry text: %q", text, res.Reply)
		}
	}
}

func TestLegacyFlowAppliesQtyRevisionAtConfirmWithoutSaving(t *testing.T) {
	st := confirmStepState(true)
	persisted := 0
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "ganti jadi 2 ya", State: &st, Catalog: omahCadburyLiveCatalog(), Profile: foodProfile(),
	}, func(OrderState) (string, error) {
		persisted++
		return "9f1c2d3e-0000-0000-0000-000000000000", nil
	})
	if res.Completed || res.Cleared || persisted != 0 {
		t.Fatalf("qty edit must not finish the order: completed=%v cleared=%v persisted=%d", res.Completed, res.Cleared, persisted)
	}
	if res.State == nil || res.State.Step != "confirm" || res.State.Qty != 2 || res.State.Items[0].Qty != 2 {
		t.Fatalf("qty must update in place at confirm: %+v", res.State)
	}
	if !strings.Contains(res.Reply, "OK") || !strings.Contains(res.Reply, "Abon Sapi 500 Gram") {
		t.Fatalf("must re-show summary after edit, got %q", res.Reply)
	}
}

func TestLegacyFlowFinishesOrderFromConfirmStepOnExplicitConfirm(t *testing.T) {
	for _, text := range []string{"ok", "Oke kak", "ya benar", "sudah benar, lanjut proses", "yes", "looks good", "👍"} {
		st := confirmStepState(true)
		persisted := 0
		res := AdvanceOrderFlow(OrderFlowInput{
			UserText: text, State: &st, Catalog: omahCadburyLiveCatalog(), Profile: foodProfile(),
		}, func(OrderState) (string, error) {
			persisted++
			return "9f1c2d3e-0000-0000-0000-000000000000", nil
		})
		if !res.Completed || !res.Cleared || persisted != 1 {
			t.Fatalf("%q is an explicit confirmation: completed=%v cleared=%v persisted=%d reply=%q",
				text, res.Completed, res.Cleared, persisted, res.Reply)
		}
		if strings.TrimSpace(res.Reply) == "" {
			t.Fatal("the buyer must get the order confirmation")
		}
	}
}

func TestLegacyFlowKeepsConfirmStateWhenPersistFails(t *testing.T) {
	st := confirmStepState(true)
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "ok", State: &st, Catalog: omahCadburyLiveCatalog(), Profile: foodProfile(),
	}, func(OrderState) (string, error) { return "", errors.New("persist failed") })
	if res.Completed || res.Cleared || res.State == nil {
		t.Fatalf("a failed save must keep the draft: %+v", res)
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
		t.Fatalf("incomplete shipping must go back to ask_address_full: %+v", res)
	}
	if strings.Contains(res.Reply, "data ordernya") {
		t.Fatal("must not answer with the generic retry text")
	}
}

func TestIsExplicitOrderConfirm(t *testing.T) {
	yes := []string{
		"ok", "OK", "Oke", "oke kak", "ok.", "ya", "iya kak", "ya benar", "betul", "sudah benar", "udah bener kak",
		"lanjut", "lanjut proses", "silakan proses", "setuju", "yes", "yes please", "confirm", "go ahead",
		"looks good", "sounds good", "that's right", "ok thanks", "sip", "gas aja", "👍",
	}
	for _, s := range yes {
		if !IsExplicitOrderConfirm(s) {
			t.Errorf("%q should be an explicit confirmation", s)
		}
	}
	no := []string{
		"", "   ", "hmm", "tidak", "nggak", "belum", "jangan", "ok tapi ganti alamat", "oke berapa ongkirnya?",
		"ya tapi tambah 1", "batal", "cancel", "ganti alamat", "bukan", "ok ok ok ok ok ok ok ok ok ok",
		"ya nggak jadi", "not ok", "no thanks", "terima kasih", "thanks", "makasih",
	}
	for _, s := range no {
		if IsExplicitOrderConfirm(s) {
			t.Errorf("%q must not be an explicit confirmation", s)
		}
	}
}
