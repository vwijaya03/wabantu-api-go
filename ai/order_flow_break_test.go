package ai

import "testing"

func TestShouldBreakOrderFlowPriceQuestion(t *testing.T) {
	if !ShouldBreakOrderFlow("mau tanya jeans jiniso XL berapa harganya ?", "ask_address", nil) {
		t.Fatal("price question should exit order flow")
	}
}

func TestShouldBreakOrderFlowGreeting(t *testing.T) {
	if !ShouldBreakOrderFlow("malam gan", "ask_qty", nil) {
		t.Fatal("greeting should exit order flow")
	}
}

func TestOrderFlowCancelled(t *testing.T) {
	if !IsOrderFlowCancelled("tidak jadi order") {
		t.Fatal("expected cancel detection")
	}
}

func TestShouldNotBreakOrderFlowQty(t *testing.T) {
	if ShouldBreakOrderFlow("1 pcs saja", "ask_qty", nil) {
		t.Fatal("qty reply should stay in order flow")
	}
}

func TestShouldBreakOrderFlowCatalogList(t *testing.T) {
	msgs := []string{
		"jualan apa aja ni",
		"loh kamu jualan apa saja",
		"saya tanya, kamu itu jualan apa saja ?",
		"kamu jualan apa",
	}
	for _, m := range msgs {
		if !ShouldBreakOrderFlow(m, "ask_variant", nil) {
			t.Fatalf("catalog list should break order flow: %q", m)
		}
	}
}
