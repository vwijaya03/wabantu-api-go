package proofcheck

import (
	"reflect"
	"testing"
)

func f(v float64) *float64 { return &v }

func TestResult(t *testing.T) {
	cases := []struct {
		has     bool
		amt     *float64
		flags   []string
		blocked bool
		want    string
		reasons []string
	}{
		{false, nil, nil, false, "", nil},
		{true, f(85000), nil, false, Match, nil},
		{true, f(85000), []string{"ai_review_required"}, false, Match, nil},
		{true, f(80000), []string{"mismatch_amount"}, false, NeedsReview, []string{"mismatch_amount"}},
		{true, f(85000), []string{"account_mismatch", "ai_review_required"}, false, NeedsReview, []string{"account_mismatch"}},
		{true, nil, []string{"duplicate_hash"}, false, NeedsReview, []string{"duplicate_hash", "missing_amount"}},
		{true, f(85000), nil, true, NeedsReview, []string{"proof_blocked"}},
	}
	for i, c := range cases {
		got, r := Result(c.has, c.amt, c.flags, c.blocked)
		if got != c.want || !reflect.DeepEqual(r, c.reasons) {
			t.Errorf("%d: %q %v", i, got, r)
		}
	}
}
