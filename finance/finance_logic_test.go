package finance

import (
	"database/sql"
	"testing"
)

// ── walletPeriod ──────────────────────────────────────────────────────────────

func TestWalletPeriod(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"2025-01-15", "2025-01"},
		{"2024-12-31", "2024-12"},
		{"2025-06-01", "2025-06"},
		// Short or empty strings fall back to current month (not blank)
		{"2025", ""},    // len < 7 → fallback, skip exact value
		{"", ""},        // empty → fallback, skip exact value
	}
	for _, c := range cases {
		got := walletPeriod(c.in)
		if c.want != "" && got != c.want {
			t.Errorf("walletPeriod(%q) = %q, want %q", c.in, got, c.want)
		}
		if c.want == "" && got == "" {
			t.Errorf("walletPeriod(%q) returned empty, expected a fallback YYYY-MM", c.in)
		}
	}
}

func TestWalletPeriodLengthExactly7(t *testing.T) {
	got := walletPeriod("2025-06-13")
	if len(got) != 7 {
		t.Fatalf("walletPeriod length = %d, want 7 (got %q)", len(got), got)
	}
}

// ── moneyString ───────────────────────────────────────────────────────────────

func TestMoneyString(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1000, "1000.00"},
		{1000.5, "1000.50"},
		{0.001, "0.00"},   // rounds to zero
		{-0.001, "0.00"},  // rounds to zero
		{0, "0.00"},
		{150000.999, "150001.00"}, // standard rounding
	}
	for _, c := range cases {
		got := moneyString(c.in)
		if got != c.want {
			t.Errorf("moneyString(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── nullStr ───────────────────────────────────────────────────────────────────

func TestNullStr(t *testing.T) {
	// Valid NullString → returns the string value
	valid := sql.NullString{String: "penjualan", Valid: true}
	got := nullStr(valid)
	if got != "penjualan" {
		t.Fatalf("nullStr(valid) = %v, want %q", got, "penjualan")
	}

	// Invalid NullString → returns nil
	invalid := sql.NullString{Valid: false}
	if nullStr(invalid) != nil {
		t.Fatal("nullStr(invalid) should return nil")
	}
}

// ── parsePostgreSQLStringArray ────────────────────────────────────────────────

func TestParsePostgreSQLStringArrayEmpty(t *testing.T) {
	cases := []sql.NullString{
		{Valid: false},
		{String: "", Valid: true},
		{String: "{}", Valid: true},
		{String: "  ", Valid: true},
	}
	for _, c := range cases {
		got := parsePostgreSQLStringArray(c)
		if len(got) != 0 {
			t.Errorf("parsePostgreSQLStringArray(%+v) = %v, want []", c, got)
		}
	}
}

func TestParsePostgreSQLStringArrayPostgresFormat(t *testing.T) {
	raw := sql.NullString{String: `{alpha,beta,gamma}`, Valid: true}
	got := parsePostgreSQLStringArray(raw)
	if len(got) != 3 {
		t.Fatalf("want 3 elements, got %v", got)
	}
	if got[0] != "alpha" || got[1] != "beta" || got[2] != "gamma" {
		t.Fatalf("unexpected elements: %v", got)
	}
}

func TestParsePostgreSQLStringArrayQuoted(t *testing.T) {
	raw := sql.NullString{String: `{"tag one","tag two"}`, Valid: true}
	got := parsePostgreSQLStringArray(raw)
	if len(got) != 2 {
		t.Fatalf("want 2 elements, got %v", got)
	}
	if got[0] != "tag one" || got[1] != "tag two" {
		t.Fatalf("unexpected elements: %v", got)
	}
}

func TestParsePostgreSQLStringArrayJSONFormat(t *testing.T) {
	raw := sql.NullString{String: `["x","y"]`, Valid: true}
	got := parsePostgreSQLStringArray(raw)
	if len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Fatalf("JSON array parsing failed: %v", got)
	}
}

// ── staffNeedsApproval ────────────────────────────────────────────────────────

func TestStaffNeedsApprovalDisabled(t *testing.T) {
	cfg := approvalConfig{enabled: false}
	if staffNeedsApproval(cfg, "expense", 999999) {
		t.Fatal("should not require approval when disabled")
	}
}

func TestStaffNeedsApprovalBelowThreshold(t *testing.T) {
	cfg := approvalConfig{
		enabled:   true,
		threshold: sql.NullFloat64{Float64: 500000, Valid: true},
	}
	if staffNeedsApproval(cfg, "expense", 100000) {
		t.Fatal("should not require approval below threshold")
	}
}

func TestStaffNeedsApprovalAboveThreshold(t *testing.T) {
	cfg := approvalConfig{
		enabled:   true,
		threshold: sql.NullFloat64{Float64: 500000, Valid: true},
	}
	if !staffNeedsApproval(cfg, "expense", 500001) {
		t.Fatal("should require approval above threshold")
	}
}

func TestStaffNeedsApprovalTypeRestriction(t *testing.T) {
	cfg := approvalConfig{
		enabled:         true,
		requireForTypes: []string{"expense"},
	}
	if staffNeedsApproval(cfg, "income", 999999) {
		t.Fatal("income type should not need approval when only expense is restricted")
	}
	if !staffNeedsApproval(cfg, "expense", 1) {
		t.Fatal("expense should require approval when type is listed")
	}
}

func TestStaffNeedsApprovalNoTypeRestriction(t *testing.T) {
	// Empty requireForTypes means all types need approval
	cfg := approvalConfig{
		enabled:         true,
		requireForTypes: []string{},
	}
	if !staffNeedsApproval(cfg, "income", 1) {
		t.Fatal("all types should require approval when requireForTypes is empty")
	}
}

func TestStaffNeedsApprovalThresholdNotValid(t *testing.T) {
	// Threshold null → amount check is skipped, so any amount needs approval
	cfg := approvalConfig{
		enabled:   true,
		threshold: sql.NullFloat64{Valid: false},
	}
	if !staffNeedsApproval(cfg, "expense", 1) {
		t.Fatal("should require approval when threshold is not set")
	}
}
