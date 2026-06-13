package ai

import (
	"fmt"
	"testing"
)

func TestWABuyerCases2000(t *testing.T) {
	cases := generateAllWABuyerCases()
	if len(cases) < 2000 {
		t.Fatalf("expected >= 2000 cases, got %d", len(cases))
	}
	stats := map[string]int{}
	var failures []string
	for _, c := range cases {
		stats[c.Category]++
		name := fmt.Sprintf("%04d_%s_%s", c.ID, c.Category, c.LanguageStyle)
		t.Run(name, func(t *testing.T) {
			actual := EvaluateWABuyerCase(c)
			if err := c.Assert(actual); err != nil {
				if c.Adversarial {
					t.Logf("ADVERSARIAL miss (document if known gap): %v | in=%q state=%s", err, c.InputUser, c.ExpectedState)
				}
				t.Fatal(err)
			}
		})
	}
	t.Logf("total=%d categories=%v", len(cases), stats)
	_ = failures
}

func TestWABuyerCaseCount(t *testing.T) {
	n := len(generateAllWABuyerCases())
	if n < 2000 {
		t.Fatalf("generator returned %d, need 2000+", n)
	}
}
