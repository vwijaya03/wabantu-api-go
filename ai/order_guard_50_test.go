package ai

import (
	"fmt"
	"testing"
)

func TestOrderGuard50(t *testing.T) {
	skipUnlessIntegrationTests(t)
	cases := allOrderGuardCases()
	if len(cases) != 50 {
		t.Fatalf("expected 50 order guard cases, got %d", len(cases))
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

func TestProductionThreadRegression(t *testing.T) {
	msg := "mau order boxer mono spot 10 paket bisa ?"
	if IsOrderStatusInquiry(msg) {
		t.Fatal("must not treat new purchase as order status")
	}
	if !IsConsultingPurchaseQuestion(msg) {
		t.Fatal("must be consulting purchase question")
	}
	clarify := "order mana yang kamu batalkan ?"
	if IsOrderCancelRequest(clarify) {
		t.Fatal("cancel clarification must not trigger cancel request")
	}
}
