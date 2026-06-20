package ai

import (
	"fmt"
	"testing"
)

func TestOrderStatusBuyer30(t *testing.T) {
	skipUnlessIntegrationTests(t)
	cases := allOrderStatusBuyerCases()
	if len(cases) != 30 {
		t.Fatalf("expected 30 cases, got %d", len(cases))
	}
	stats := map[string]int{}
	for _, c := range cases {
		stats[c.Category]++
		name := fmt.Sprintf("%03d_%s_%s", c.ID, c.Category, c.Name)
		t.Run(name, func(t *testing.T) {
			if c.Pure != nil {
				if err := c.Pure(); err != nil {
					t.Fatal(err)
				}
				return
			}
			sim := newOmahSimulator()
			outcomes := c.Run(sim)
			if c.Assert != nil {
				if err := c.Assert(c, outcomes); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	t.Logf("categories: %v", stats)
}

func TestGreetingWithOrderStatusNotGreeting(t *testing.T) {
	msg := "halo min apakah saya punya pesanan aktif ?"
	if IsGreetingLike(msg) {
		t.Fatal("must route to order status, not greeting")
	}
	if !IsOrderStatusInquiry(msg) {
		t.Fatal("must be order status inquiry")
	}
	if !WantsActiveOrderOnly(msg) {
		t.Fatal("must want active orders only")
	}
}
