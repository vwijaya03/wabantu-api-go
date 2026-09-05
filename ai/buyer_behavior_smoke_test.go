package ai

import "testing"

func TestBuyerBehaviorScriptHappyAbon(t *testing.T) {
	sim := newOmahSimulator()
	o := sim.RunScript(
		"saya jadi beli abon sapi 500g 2 pcs",
		recipientBlock("Budi", "081234567890"),
		fullAddressBlock(),
	)
	if !o[len(o)-1].Completed {
		t.Fatalf("expected complete, last=%+v", o[len(o)-1])
	}
}

func TestBuyerBehaviorRevisionNotCancel(t *testing.T) {
	sim := newOmahSimulator()
	sim.RunScript("saya jadi beli abon sapi 1 pcs")
	last := sim.Turn("ga jadi mau dirubah menjadi 10 biji ya")
	if last.Canceled {
		t.Fatal("revision should not cancel")
	}
	if last.Order == nil || last.Order.Qty != 10 {
		t.Fatalf("expected qty 10, order=%+v", last.Order)
	}
}
