package ai

import "testing"

// Regression tests moved to internal/buyerflow (go test, <10s CI).
// Encore smoke on master still runs TestConversationRegression via re-export.

func TestConversationRegression(t *testing.T) {
	t.Run("delegate", func(t *testing.T) {
		sim := newOmahSimulator()
		out := sim.Turn("bisa minta nomor rekeningnya ga sih ?")
		if out.Path != PathPaymentFAQ {
			t.Fatalf("path = %q want %q", out.Path, PathPaymentFAQ)
		}
	})
}

func TestConversationRegressionScript(t *testing.T) {
	sim := newOmahSimulator()
	out := sim.Turn("mau order abon sapi yang 500 gram 3 biji ya")
	if out.Path != PathOrderFlow {
		t.Fatalf("path = %q want %q", out.Path, PathOrderFlow)
	}
}
