package inventory

import "testing"

func TestCommittedStatusesForSQL(t *testing.T) {
	got := committedStatusesForSQL()
	if len(got) != len(committedOrderStatuses) {
		t.Fatalf("len = %d, want %d", len(got), len(committedOrderStatuses))
	}
	// must be sorted (stable SQL) and contain the committed set
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("not sorted: %v", got)
		}
	}
	for _, s := range got {
		if !IsCommittedOrderStatus(s) {
			t.Fatalf("%q not committed", s)
		}
	}
}
