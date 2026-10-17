package salesbrain

import (
	"strings"
	"testing"
)

func TestRedactForLog(t *testing.T) {
	in := `{"reply":"Thanks Ana, phone 0812 3456 7890 and +62 812-3456-7890 noted; email ana@example.com; total Rp425.000"}`
	got := RedactForLog(in, 1000)
	for _, leak := range []string{"0812 3456 7890", "812-3456-7890", "ana@example.com"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
	if !strings.Contains(got, "Rp425.000") {
		t.Errorf("amounts must survive: %q", got)
	}
	if len([]rune(RedactForLog(strings.Repeat("a", 2000), 1000))) > 1001 {
		t.Error("must truncate")
	}
}

func TestRedactForLogEdgeCases(t *testing.T) {
	// Large amounts have 8+ digits but are money, not identifiers.
	if got := RedactForLog("Total Rp12.500.000 (shipping not included)", 1000); got != "Total Rp12.500.000 (shipping not included)" {
		t.Errorf("large Rp amount changed: %q", got)
	}
	// A phone right after a protected amount is still redacted.
	if got := RedactForLog("Rp12.500.000 0812 3456 7890", 1000); got != "Rp12.500.000 [NUM]" {
		t.Errorf("phone after an amount: %q", got)
	}
	// Digits inside an email go away together with the address.
	if got := RedactForLog("mail ana12345678@example.com now", 1000); got != "mail [EMAIL] now" {
		t.Errorf("email with digits: %q", got)
	}
	// Dots and parentheses are allowed as separators between digits.
	for _, in := range []string{"call (021) 555 1234 now", "call 0812.3456.7890 now"} {
		got := RedactForLog(in, 1000)
		if !strings.Contains(got, "[NUM]") || strings.Contains(got, "555") || strings.Contains(got, "3456") {
			t.Errorf("separator variant leaked: %q -> %q", in, got)
		}
	}
	// Short numbers (quantities, prices, postal codes, times) are not identifiers.
	keep := "2x item, postal code 60177, Rp75.000, open 08:00 - 19:00"
	if got := RedactForLog(keep, 1000); got != keep {
		t.Errorf("short numbers changed: %q", got)
	}
	if got := RedactForLog("", 1000); got != "" {
		t.Errorf("empty in, empty out, got %q", got)
	}
}

func TestRedactForLogTruncatesAfterRedacting(t *testing.T) {
	// Redact first, then cut: a cut must never leave half a phone number behind.
	// "aaaaaaaaaa [NUM]" cut to 12 runes is "aaaaaaaaaa [" plus the ellipsis.
	got := RedactForLog(strings.Repeat("a", 10)+" 0812 3456 7890", 12)
	if got != "aaaaaaaaaa […" {
		t.Errorf("got %q", got)
	}
	got = RedactForLog(strings.Repeat("a", 10)+" 0812 3456 7890", 14)
	if got != "aaaaaaaaaa [NU…" {
		t.Errorf("got %q", got)
	}
	if n := len([]rune(RedactForLog(strings.Repeat("é", 50), 10))); n != 11 {
		t.Errorf("must cut on runes, not bytes: got %d runes", n)
	}
}
