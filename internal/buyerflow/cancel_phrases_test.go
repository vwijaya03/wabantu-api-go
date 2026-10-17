package buyerflow

import "testing"

func TestWholeOrderCancelPhrases(t *testing.T) {
	for _, s := range []string{"please cancel my previous order", "cancel my previous order", "please cancel", "cancel the order", "can i cancel my order b", "batalkan pesanan sebelumnya", "batalin pesanan", "never mind"} {
		if !IsOrderCancelRequest(s) {
			t.Errorf("IsOrderCancelRequest(%q) = false, want true", s)
		}
		if IsCartLineCorrectionIntent(s) {
			t.Errorf("IsCartLineCorrectionIntent(%q) = true, want false (whole-order cancel)", s)
		}
	}
	for _, s := range []string{"cancel the tuna", "batalkan abon sapi", "remove the salmon"} {
		if !IsCartLineCorrectionIntent(s) {
			t.Errorf("IsCartLineCorrectionIntent(%q) = false, want true", s)
		}
	}
}

// "please cancel" alone is a whole-order cancel, but with a product it stays a line cancel
// (the phrase must not be matched as a substring, or "please cancel the tuna" would wipe the cart).
func TestPoliteLineCancelStaysLineCorrection(t *testing.T) {
	for _, s := range []string{"please cancel the tuna", "please cancel 1 tuna", "pls cancel salmon", "can you cancel the salmon", "delete the tuna", "hapus abon sapi"} {
		if !IsCartLineCorrectionIntent(s) {
			t.Errorf("IsCartLineCorrectionIntent(%q) = false, want true", s)
		}
		if IsOrderCancelRequest(s) {
			t.Errorf("IsOrderCancelRequest(%q) = true, want false (line cancel keeps the cart)", s)
		}
	}
}

func TestSimulatorWholeCancelVersusLineRemove(t *testing.T) {
	for _, msg := range []string{"please cancel my previous order", "batalkan pesanan sebelumnya", "please cancel"} {
		s := newOmahSteakSimulator()
		s.SavedBuyerLang = LangEN
		s.Turn("Okay, can i order 1 tuna steak please")
		if s.Order == nil {
			t.Fatal("no draft order started")
		}
		if out := s.Turn(msg); !out.Canceled || s.Order != nil {
			t.Errorf("%q: canceled=%v order=%+v, want whole order canceled", msg, out.Canceled, s.Order)
		}
	}

	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	s.Turn("Okay, can i order 1 tuna steak please")
	s.Turn("i want to add 1 salmon steak 100 gram")
	if s.Order == nil || len(s.Order.Items) != 2 {
		t.Fatalf("setup: want 2 lines, got %+v", s.Order)
	}
	out := s.Turn("remove the salmon")
	if out.Canceled || s.Order == nil || len(s.Order.Items) != 1 || s.Order.Items[0].CatalogItemID != "tuna-100" {
		t.Fatalf("remove the salmon must drop only that line, canceled=%v order=%+v", out.Canceled, s.Order)
	}
}
