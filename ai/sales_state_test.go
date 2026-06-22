package ai

import "testing"

func TestIsConsultingPurchaseQuestion(t *testing.T) {
	consulting := []string{
		"boleh beli 1 pcs ?",
		"kalau order satu bisa?",
		"misal saya beli 2 gimana?",
		"loh ga mau checkout dulu masih mau tanya boleh bijian nggak",
		"boleh eceran ga",
	}
	for _, m := range consulting {
		if !IsConsultingPurchaseQuestion(m, nil) {
			t.Fatalf("expected consulting: %q", m)
		}
		if HasPurchaseIntent(m) {
			t.Fatalf("consulting should not be purchase intent: %q", m)
		}
	}
}

func TestHasPurchaseIntentExplicitOnly(t *testing.T) {
	if !HasPurchaseIntent("saya jadi beli boxer mono spot L") {
		t.Fatal("expected explicit cart ready")
	}
	if HasPurchaseIntent("mau beli boxer bisa ga") {
		t.Fatal("question with beli should not be purchase intent")
	}
}

func TestIsUserSalesCorrection(t *testing.T) {
	msgs := []string{
		"loh saya masih tanya jangan di checkoutkan dulu",
		"ha ?",
		"belum order",
	}
	for _, m := range msgs {
		if !IsUserSalesCorrection(m) {
			t.Fatalf("expected correction: %q", m)
		}
		if !ShouldBreakOrderFlow(m, "ask_variant", nil) {
			t.Fatalf("should break order flow: %q", m)
		}
	}
}

func TestWouldRepeatOutbound(t *testing.T) {
	hist := []dbMessage{{
		Direction: "out",
		Body:      "Oke kak. Ukuran (S/M/L/XL) dan warnanya apa?",
	}}
	if !wouldRepeatOutbound(hist, "Oke kak. Ukuran (S/M/L/XL) dan warnanya apa?") {
		t.Fatal("expected repeat detection")
	}
}
