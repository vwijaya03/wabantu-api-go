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

func TestSlotStartMatches(t *testing.T) {
	if !slotStartMatches("09:00:00", "09.00") {
		t.Fatal("expected match")
	}
	if slotStartMatches("10:00:00", "09:00") {
		t.Fatal("expected no match")
	}
}
