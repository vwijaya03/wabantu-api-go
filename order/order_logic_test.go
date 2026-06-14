package order

import (
	"fmt"
	"strings"
	"testing"
)

// ── batchIDs ─────────────────────────────────────────────────────────────────

func TestBatchIDsBasic(t *testing.T) {
	placeholders, args := batchIDs([]string{"a", "b", "c"}, 1)
	if len(placeholders) != 3 || len(args) != 3 {
		t.Fatalf("want 3 placeholders/args, got %d/%d", len(placeholders), len(args))
	}
	for i, p := range placeholders {
		want := fmt.Sprintf("$%d", i+1)
		if p != want {
			t.Fatalf("placeholder[%d] = %q, want %q", i, p, want)
		}
	}
}

// BatchDelete uses start=1 for the first SELECT query and start=2 for the
// DELETE (with $1 = uid).  Verify both modes produce the right numbering.
func TestBatchIDsStartParam(t *testing.T) {
	// start=1: $1, $2, $3 – used in SELECT query (no uid prefix)
	ph1, _ := batchIDs([]string{"x", "y"}, 1)
	if ph1[0] != "$1" || ph1[1] != "$2" {
		t.Fatalf("start=1 placeholders: %v", ph1)
	}

	// start=2: $2, $3 – used in DELETE query where $1 = uid
	ph2, _ := batchIDs([]string{"x", "y"}, 2)
	if ph2[0] != "$2" || ph2[1] != "$3" {
		t.Fatalf("start=2 placeholders: %v", ph2)
	}
}

func TestBatchIDsFiltersBlankIDs(t *testing.T) {
	placeholders, args := batchIDs([]string{"", "  ", "valid-id", ""}, 1)
	if len(placeholders) != 1 || len(args) != 1 {
		t.Fatalf("want only 1 valid ID, got %d placeholders / %d args", len(placeholders), len(args))
	}
	if args[0] != "valid-id" {
		t.Fatalf("args[0] = %v, want 'valid-id'", args[0])
	}
}

func TestBatchIDsAllBlank(t *testing.T) {
	placeholders, args := batchIDs([]string{"", "   "}, 1)
	if len(placeholders) != 0 || len(args) != 0 {
		t.Fatalf("expected empty result for all-blank IDs, got %d/%d", len(placeholders), len(args))
	}
}

// ── nullUUIDArg ───────────────────────────────────────────────────────────────

func TestNullUUIDArgEmpty(t *testing.T) {
	if nullUUIDArg("") != nil {
		t.Fatal("empty string should map to nil")
	}
	if nullUUIDArg("   ") != nil {
		t.Fatal("whitespace-only string should map to nil")
	}
}

func TestNullUUIDArgNonEmpty(t *testing.T) {
	const id = "some-uuid-value"
	got := nullUUIDArg(id)
	if got != id {
		t.Fatalf("nullUUIDArg(%q) = %v, want %q", id, got, id)
	}
}

// ── joinStrings ───────────────────────────────────────────────────────────────

func TestJoinStrings(t *testing.T) {
	cases := []struct {
		in   []string
		sep  string
		want string
	}{
		{[]string{"a", "b", "c"}, ", ", "a, b, c"},
		{[]string{"x"}, ", ", "x"},
		{[]string{}, ", ", ""},
		{[]string{"a", "b"}, " AND ", "a AND b"},
	}
	for _, c := range cases {
		got := joinStrings(c.in, c.sep)
		if got != c.want {
			t.Fatalf("joinStrings(%v, %q) = %q, want %q", c.in, c.sep, got, c.want)
		}
	}
}

// ── validOrderStatuses ────────────────────────────────────────────────────────

func TestShouldPrecheckBatchStockTransition(t *testing.T) {
	if !shouldPrecheckBatchStockTransition("completed", "draft") {
		t.Fatal("draft -> completed should precheck")
	}
	if shouldPrecheckBatchStockTransition("completed", "completed") {
		t.Fatal("already completed should not precheck")
	}
	if shouldPrecheckBatchStockTransition("completed", "processing") {
		t.Fatal("processing -> completed should not precheck (stock already issued)")
	}
	if !shouldPrecheckBatchStockTransition("processing", "draft") {
		t.Fatal("draft -> processing should precheck")
	}
}

func TestValidOrderStatusesContainsExpected(t *testing.T) {
	required := []string{"draft", "processing", "shipped", "completed", "cancelled"}
	for _, s := range required {
		if !validOrderStatuses[s] {
			t.Errorf("expected %q in validOrderStatuses", s)
		}
	}
}

func TestValidOrderStatusesLegacyReadable(t *testing.T) {
	if !validOrderStatuses["confirmed"] || !validOrderStatuses["paid"] {
		t.Fatal("legacy statuses confirmed/paid should be readable")
	}
}

func TestValidOrderStatusesRejectsUnknown(t *testing.T) {
	unknowns := []string{"", "DRAFT", "Done", "deleted", "pending"}
	for _, s := range unknowns {
		if validOrderStatuses[s] {
			t.Errorf("status %q should not be valid", s)
		}
	}
}

// ── orderSelectCols ───────────────────────────────────────────────────────────

// orderSelectCols should produce SQL that includes every field the scan
// functions expect.  This is a lightweight sanity check that ensures new
// columns (e.g. income_wallet_id from PR #12) appear in the generated SQL
// when the column is added to the query builder.
func TestOrderSelectColsNoPrefixContainsRequiredFields(t *testing.T) {
	cols := orderSelectCols("")
	required := []string{
		"id", "conversation_id", "contact_id", "items",
		"shipping_address", "notes", "status",
		"tracking_number", "courier",
		"payment_transaction_id", "subtotal", "shipping_cost", "total",
		"income_wallet_id", "created_by", "created_at", "updated_at",
	}
	for _, f := range required {
		if !strings.Contains(cols, f) {
			t.Errorf("orderSelectCols() missing field %q", f)
		}
	}
}

func TestOrderSelectColsWithPrefix(t *testing.T) {
	cols := orderSelectCols("o")
	if !strings.Contains(cols, "o.id") {
		t.Fatal("prefix should appear before column names")
	}
	if strings.Contains(cols, " id,") && !strings.Contains(cols, "o.id") {
		t.Fatal("unprefixed id found with prefix set")
	}
}
