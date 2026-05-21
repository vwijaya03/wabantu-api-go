package ai

import "testing"

func TestShouldBreakOrderFlowPriceQuestion(t *testing.T) {
	if !ShouldBreakOrderFlow("mau tanya jeans jiniso XL berapa harganya ?", "ask_address") {
		t.Fatal("price question should exit order flow")
	}
}

func TestShouldBreakOrderFlowGreeting(t *testing.T) {
	if !ShouldBreakOrderFlow("malam gan", "ask_qty") {
		t.Fatal("greeting should exit order flow")
	}
}

func TestOrderFlowCancelled(t *testing.T) {
	if !IsOrderFlowCancelled("tidak jadi order") {
		t.Fatal("expected cancel detection")
	}
}

func TestShouldNotBreakOrderFlowQty(t *testing.T) {
	if ShouldBreakOrderFlow("1 pcs saja", "ask_qty") {
		t.Fatal("qty reply should stay in order flow")
	}
}
