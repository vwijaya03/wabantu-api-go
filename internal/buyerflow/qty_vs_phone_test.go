package buyerflow

import "testing"

func TestPhoneAndHouseNumbersAreNotQty(t *testing.T) {
	for _, step := range []string{"ask_recipient", "ask_address_full"} {
		for _, msg := range []string{"Name: Budi\nPhone: 0812 3456 7890", "+62 812 3456 7890", "Jl. Melati 12", "Jl. Melati No 12"} {
			st := &OrderState{Step: step, Qty: 1, ProductName: "Yellow Tail Tuna Portion 100 Gram", CatalogItemID: "tuna-100", UnitPrice: 350000}
			if tryApplyQtyRevision(st, msg) || st.Qty != 1 {
				t.Errorf("step %s msg %q changed qty to %d", step, msg, st.Qty)
			}
		}
	}
	st := &OrderState{Step: "ask_recipient", Qty: 1, ProductName: "Yellow Tail Tuna Portion 100 Gram", CatalogItemID: "tuna-100"}
	if !tryApplyQtyRevision(st, "jadi 2 pcs aja") || st.Qty != 2 {
		t.Fatalf("explicit qty change must still work, qty=%d", st.Qty)
	}
}

func tunaQtyState(step string) *OrderState {
	return &OrderState{Step: step, Qty: 1, ProductName: "Yellow Tail Tuna Portion 100 Gram", CatalogItemID: "tuna-100", UnitPrice: 350000}
}

// A unit word only counts as a quantity cue when it is attached to a number, so street names
// ("Jl. Buah Batu") and ordinary words ("packing", "capcay") do not make house numbers a qty.
func TestStreetNamesWithUnitWordsAreNotQty(t *testing.T) {
	for _, step := range []string{"ask_recipient", "ask_address", "ask_address_full"} {
		for _, msg := range []string{"Jl. Buah Batu No 12", "Jl. Capcay 12", "packing yang rapi, hp 081234567890", "60177"} {
			st := tunaQtyState(step)
			if tryApplyQtyRevision(st, msg) || st.Qty != 1 {
				t.Errorf("step %s msg %q changed qty to %d", step, msg, st.Qty)
			}
		}
	}
}

func TestExplicitQtyCuesStillReviseAtRecipientAndAddressSteps(t *testing.T) {
	cases := map[string]int{
		"ubah qty jadi 3": 3, "make it 3": 3, "change to 2": 2, "change it to 4": 4,
		"ganti jadi 4 pcs": 4, "3pcs": 3, "dua pcs aja": 2, "2 x": 2, "qty 4": 4, "3 buah": 3, "pcs 3": 3,
	}
	for _, step := range []string{"ask_recipient", "ask_address_full"} {
		for msg, want := range cases {
			st := tunaQtyState(step)
			if !tryApplyQtyRevision(st, msg) || st.Qty != want {
				t.Errorf("step %s msg %q: qty=%d, want %d", step, msg, st.Qty, want)
			}
		}
	}
}

func TestBareNumberStillSetsQtyAtAskQty(t *testing.T) {
	st := tunaQtyState("ask_qty")
	if !tryApplyQtyRevision(st, "3 aja") || st.Qty != 3 {
		t.Fatalf("the cue gate only applies to recipient/address steps, qty=%d", st.Qty)
	}
}
