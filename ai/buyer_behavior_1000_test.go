package ai

import (
	"fmt"
	"testing"
)

func TestBuyerBehavior1000(t *testing.T) {
	skipUnlessIntegrationTests(t)
	cases := allBuyerBehaviorCases()
	if len(cases) != 1000 {
		t.Fatalf("expected 1000 cases, got %d", len(cases))
	}
	stats := map[string]int{}
	for _, c := range cases {
		stats[c.Category]++
		t.Run(fmt.Sprintf("%04d_%s_%s", c.ID, c.Category, c.Name), func(t *testing.T) {
			sim := newOmahSimulator()
			var outcomes []TurnOutcome
			if c.Run != nil {
				outcomes = c.Run(sim)
			}
			if c.Assert != nil {
				if err := c.Assert(c, outcomes); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	t.Logf("categories: %v", stats)
}
