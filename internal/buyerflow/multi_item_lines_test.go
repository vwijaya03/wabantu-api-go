package buyerflow

import (
	"strings"
	"testing"
)

func TestUnitVariantsMatchCatalog(t *testing.T) {
	cat := omahSteakCatalog()
	cases := map[string]string{"1 of Australian beef 500grams": "beef-500", "1 salmon steak 100gr": "salmon-100", "salmon 250 grams": "salmon-250"}
	for msg, want := range cases {
		if it := matchCatalogItem(msg, cat); it == nil || it.ID != want {
			t.Errorf("matchCatalogItem(%q) = %v, want %s", msg, it, want)
		}
	}
}

func TestOrderSegmentsSurviveTrailingQuestion(t *testing.T) {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	s.Turn("Okay, can i order 1 tuna steak and 1 australian beef 500gr please?  And can you do same day delivery?")
	if s.Order == nil {
		t.Fatal("no order started")
	}
	ids := map[string]bool{}
	for _, l := range s.Order.Items {
		ids[l.CatalogItemID] = true
	}
	if s.Order.CatalogItemID != "" {
		ids[s.Order.CatalogItemID] = true
	}
	if !ids["tuna-100"] || !ids["beef-500"] {
		t.Fatalf("both lines must be kept, got %+v", s.Order)
	}
}

func TestAmbiguousAppendIsAskedNotDropped(t *testing.T) {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	s.Turn("Okay, can i order 1 tuna steak please?")
	out := s.Turn("i want too add Salmon steak 100 gr, and australian beef too")
	if !strings.Contains(out.Reply, "T-Bone") || !strings.Contains(out.Reply, "500 Gram") {
		t.Fatalf("ambiguous 'australian beef' must be asked with its two variants, reply=%q", out.Reply)
	}
}
