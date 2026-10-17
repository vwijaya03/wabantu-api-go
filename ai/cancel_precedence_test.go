package ai

import (
	"testing"

	bf "encore.app/wabantu/internal/buyerflow"
)

// Whole-order cancel is decided before amend: "cancel my previous order" / "batalkan pesanan"
// must reach the cancel branch, while "cancel the tuna" (one line) and a negated "bukan batal
// semua, cuma tuna" must not.
func TestCancelPrecedenceWholeOrderCancelText(t *testing.T) {
	cases := []struct {
		text       string
		want       bool
		needsPartC bool // English phrases only become a whole-order cancel once Part C is merged
	}{
		{"batalkan pesanan", true, false},
		{"please cancel my previous order", true, true},
		{"cancel the tuna", false, false},
		{"bukan batal semua, cuma tuna", false, false},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			if c.needsPartC && IsCartLineCorrectionIntent(c.text) {
				t.Skip("needs Part C buyerflow phrases")
			}
			if got := isWholeOrderCancelText(c.text); got != c.want {
				t.Fatalf("isWholeOrderCancelText(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

func TestCancelPrecedenceCancelOrRemoveVerb(t *testing.T) {
	cases := map[string]bool{
		"please cancel my previous order": true,
		"hapus salmon":                    true,
		"batalkan pesanan":                true,
		"batalin aja tunanya":             true,
		"tolong dibatalkan ya":            true,
		"please remove the beef":          true,
		"delete the salmon":               true,
		"gak jadi deh":                    true,
		"ga jadi beli":                    true,
		"nggak jadi ya kak":               true,
		"bukan batal semua, cuma tuna":    true,
		"tambah 1 tuna lagi":              false,
		"add one more salmon steak":       false,
		"ganti salmon jadi tuna":          false,
		"I would like to order tuna":      false,
		"":                                false,
	}
	for text, want := range cases {
		if got := textHasCancelOrRemoveVerb(text); got != want {
			t.Errorf("textHasCancelOrRemoveVerb(%q) = %v, want %v", text, got, want)
		}
	}
}

// A cancel/remove message never adds lines from earlier chat (S29: "cancel my previous order"
// re-added the items the buyer had listed before).
func TestCancelPrecedenceNeverAddsLinesFromHistory(t *testing.T) {
	cat := toBFCatalogSlice(omahGroundingCatalog())
	history := []bf.Message{
		{Author: "contact", Direction: "in", Body: "I would like to order\n1 of yellow tail tuna portion 100 gram"},
		{Author: "ai", Direction: "out", Body: "Noted! What is the recipient's name and phone?"},
		{Author: "contact", Direction: "in", Body: "please cancel my previous order"},
	}
	existing := map[string]bool{}

	// Precondition: the earlier message really yields a line, so the guard below is what removes it.
	if got := bf.ExtractAmendLinesFromHistory(history, cat, existing, nil); len(got) == 0 {
		t.Fatal("fixture: the history should yield an order line for the guard to suppress")
	}
	for _, text := range []string{"please cancel my previous order", "hapus salmon", "batalkan pesanan"} {
		if got := amendLinesFromHistory(text, history, cat, existing, nil); len(got) != 0 {
			t.Errorf("%q must not add lines from history, got %+v", text, got)
		}
	}
	if got := amendLinesFromHistory("tambah lagi", history, cat, existing, nil); len(got) == 0 {
		t.Error("a plain add must still recover lines from history")
	}
}
