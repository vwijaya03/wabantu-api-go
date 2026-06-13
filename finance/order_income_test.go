package finance

import (
	"context"
	"database/sql"
	"testing"
)

// ── RecordOrderCompletedIncome — early-exit guards ────────────────────────────
//
// These tests exercise the guard clauses at the top of RecordOrderCompletedIncome
// that return nil before any database call is made.  They remain valid across
// all branches (master, fix/finance-hard-delete-transactions, etc.) because the
// early-return semantics are preserved in every version of the function.

func TestRecordOrderCompletedIncomeZeroAmountNoDBCall(t *testing.T) {
	// amount <= 0 → return nil immediately, no DB connection opened
	for _, amt := range []float64{0, -1, -0.001} {
		err := RecordOrderCompletedIncome(context.Background(), "does_not_exist", "user-1", "order-uuid", amt, "")
		if err != nil {
			t.Errorf("RecordOrderCompletedIncome(amount=%v) = %v, want nil", amt, err)
		}
	}
}

func TestRecordOrderCompletedIncomeEmptyOrderIDNoDBCall(t *testing.T) {
	// empty / whitespace orderID → return nil immediately
	for _, id := range []string{"", "   ", "\t"} {
		err := RecordOrderCompletedIncome(context.Background(), "does_not_exist", "user-1", id, 50000, "")
		if err != nil {
			t.Errorf("RecordOrderCompletedIncome(orderID=%q) = %v, want nil", id, err)
		}
	}
}

// ── RemoveOrderIncomeTransaction — early-exit guard ───────────────────────────

func TestRemoveOrderIncomeTransactionEmptyOrderIDNoDBCall(t *testing.T) {
	// empty / whitespace orderID → return nil immediately, no DB connection opened
	for _, id := range []string{"", "   ", "\t"} {
		err := RemoveOrderIncomeTransaction(context.Background(), "does_not_exist", id)
		if err != nil {
			t.Errorf("RemoveOrderIncomeTransaction(orderID=%q) = %v, want nil", id, err)
		}
	}
}

// ── walletPeriod — used by CheckCurrentPeriodUnlocked ─────────────────────────
//
// walletPeriod is the date→YYYY-MM extractor used internally by period lock
// checks (PR #16).  These tests verify its semantics.

func TestWalletPeriodExtracts7Chars(t *testing.T) {
	cases := map[string]string{
		"2025-01-01": "2025-01",
		"2025-12-31": "2025-12",
		"2026-06-13": "2026-06",
	}
	for in, want := range cases {
		got := walletPeriod(in)
		if got != want {
			t.Errorf("walletPeriod(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWalletPeriodFallbackForShortInput(t *testing.T) {
	// When input is shorter than 7 chars, function falls back to current month.
	// We can't assert the exact value but it must not be empty.
	got := walletPeriod("")
	if got == "" {
		t.Fatal("walletPeriod(\"\") should return current YYYY-MM, got empty string")
	}
	if len(got) != 7 {
		t.Fatalf("walletPeriod(\"\") = %q, expected length 7 (YYYY-MM)", got)
	}
}

// ── staffNeedsApproval — approval gate used by CreateTransaction ──────────────
//
// Full test matrix for the approval gating logic (see also finance_logic_test.go
// for base cases; these focus on edge cases relevant to the finance workflow).

func TestStaffNeedsApprovalExactThreshold(t *testing.T) {
	cfg := approvalConfig{
		enabled:   true,
		threshold: sql.NullFloat64{Float64: 100000, Valid: true},
	}
	// Exactly at threshold → needs approval
	if !staffNeedsApproval(cfg, "expense", 100000) {
		t.Fatal("amount == threshold should require approval")
	}
	// One unit below → no approval
	if staffNeedsApproval(cfg, "expense", 99999) {
		t.Fatal("amount < threshold should NOT require approval")
	}
}
