package buyerflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func upsellCount(reply string) int {
	return strings.Count(reply, "You might also like") + strings.Count(reply, "Rekomendasi tambahan")
}

func TestUpsellOncePerOrderAndSingleItem(t *testing.T) {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	first := s.Turn("Okay, can i order 1 tuna steak please")
	second := s.Turn("i want to add 1 salmon steak 100 gram")
	total := upsellCount(first.Reply) + upsellCount(second.Reply)
	if total > 1 {
		t.Fatalf("upsell shown %d times, want at most once:\n1) %s\n2) %s", total, first.Reply, second.Reply)
	}
	// The block is the tail of its own reply. Scan each reply separately: scanning the two
	// replies as one string would count the numbered lines of the next reply's order summary
	// ("2. Salmon Steak 100 Gram") as a second suggestion.
	for i, reply := range []string{first.Reply, second.Reply} {
		for _, header := range []string{"You might also like", "Rekomendasi tambahan"} {
			if at := strings.Index(reply, header); at >= 0 && strings.Contains(reply[at:], "\n2. ") {
				t.Fatalf("upsell block in reply %d must contain one suggestion:\n%s", i+1, reply[at:])
			}
		}
	}
}

func TestUpsellFlagMarksOrderAndResetsForNewOrder(t *testing.T) {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	first := s.Turn("Okay, can i order 1 tuna steak please")
	if upsellCount(first.Reply) != 1 {
		t.Fatalf("first reply must show the upsell once, got %d:\n%s", upsellCount(first.Reply), first.Reply)
	}
	if s.Order == nil || !s.Order.UpsellShown {
		t.Fatalf("order must be marked UpsellShown after the block was shown, got %+v", s.Order)
	}
	if b, err := json.Marshal(s.Order); err != nil || !strings.Contains(string(b), `"upsellShown":true`) {
		t.Fatalf("flag must persist in the order JSON, got %s (err=%v)", b, err)
	}
	if out := s.Turn("cancel my order"); !out.Canceled || s.Order != nil {
		t.Fatalf("setup: order should be canceled, canceled=%v order=%+v", out.Canceled, s.Order)
	}
	again := s.Turn("Okay, can i order 1 tuna steak please")
	if upsellCount(again.Reply) != 1 {
		t.Fatalf("a new order gets its own upsell, got %d:\n%s", upsellCount(again.Reply), again.Reply)
	}
}
