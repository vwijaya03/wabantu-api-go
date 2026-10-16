package buyerflow

import (
	"strings"
	"testing"
)

func TestOmahSteakEnglishShippingFeeAndSameDay(t *testing.T) {
	sim := newOmahSteakSimulator()
	sim.SavedBuyerLang = LangEN
	msg := "Ohh how much is the delivery fee? And can you delivery the steak today?"
	out := sim.Turn(msg)
	if out.Path != PathShippingFAQ {
		t.Fatalf("path=%s want %s reply=%q", out.Path, PathShippingFAQ, out.Reply)
	}
	lower := strings.ToLower(out.Reply)
	for _, bad := range []string{"seluruh", "hari", "kirim", "pengiriman", "rp"} {
		if strings.Contains(lower, bad) {
			t.Fatalf("English reply must not contain %q: %q", bad, out.Reply)
		}
	}
	if !strings.Contains(out.Reply, "08:00") || !strings.Contains(out.Reply, "12:00") {
		t.Fatalf("expected same-day KB window in reply: %q", out.Reply)
	}
	if !strings.Contains(lower, "same day") {
		t.Fatalf("expected same-day wording: %q", out.Reply)
	}
}

func TestOmahSteakIndonesianShippingUsesKB(t *testing.T) {
	sim := newOmahSteakSimulator()
	sim.SavedBuyerLang = LangID
	out := sim.Turn("berapa lama estimasi pengiriman dan bisa kirim hari ini?")
	if out.Path != PathShippingFAQ {
		t.Fatalf("path=%s want %s", out.Path, PathShippingFAQ)
	}
	if !strings.Contains(out.Reply, "3-4") && !strings.Contains(out.Reply, "08:00") {
		t.Fatalf("expected KB shipping facts: %q", out.Reply)
	}
}
