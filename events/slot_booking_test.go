package events

import "testing"

func TestNormalizePreferredTime(t *testing.T) {
	tests := map[string]string{
		"09:00":    "09:00",
		"09.00":    "09:00",
		"9:00":     "09:00",
		"09:00:00": "09:00",
	}
	for in, want := range tests {
		if got := normalizePreferredTime(in); got != want {
			t.Fatalf("%q => %q, want %q", in, got, want)
		}
	}
}

func TestTherapyLookupCandidates(t *testing.T) {
	raw := "Terapi 5 Elemen (maksimal 9 orang, 5 sesi pagi, 4 sesi siang)"
	cands := therapyLookupCandidates(raw)
	if len(cands) != 2 {
		t.Fatalf("got %d candidates, want 2: %v", len(cands), cands)
	}
	if cands[1] != "Terapi 5 Elemen" {
		t.Fatalf("base name %q, want Terapi 5 Elemen", cands[1])
	}
}

func TestSlotStartMatches(t *testing.T) {
	if !slotStartMatches("09:00:00", "09.00") {
		t.Fatal("expected match")
	}
	if slotStartMatches("10:00:00", "09:00") {
		t.Fatal("expected no match")
	}
}
