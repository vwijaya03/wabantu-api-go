package ai

import "testing"

func TestGaJadiGantiNotCancel(t *testing.T) {
	msg := "ga jadi ganti 3 pcs bang"
	if IsOrderCancelRequest(msg) {
		t.Fatalf("revision message should not be cancel: %q", msg)
	}
	st := baseOrderAbon(1, "ask_recipient")
	if !tryApplyQtyRevision(st, msg) {
		t.Fatalf("tryApplyQtyRevision failed for %q", msg)
	}
	if st.Qty != 3 {
		t.Fatalf("qty want 3 got %d", st.Qty)
	}
	sim := newOmahSimulator()
	sim.Order = baseOrderAbon(1, "ask_recipient")
	out := sim.Turn(msg)
	if out.Canceled {
		t.Fatal("sim should not cancel revision message")
	}
	if out.Order == nil || out.Order.Qty != 3 {
		t.Fatalf("sim qty want 3 got %+v", out.Order)
	}
}
