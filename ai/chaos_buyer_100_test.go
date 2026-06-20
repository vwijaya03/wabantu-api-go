package ai

import (
	"fmt"
	"testing"
)

func TestChaosBuyer100(t *testing.T) {
	skipUnlessIntegrationTests(t)
	cases := allChaosBuyerCases()
	if len(cases) != 100 {
		t.Fatalf("expected 100 chaos cases, got %d", len(cases))
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

func TestChaosBatalStandalone(t *testing.T) {
	if !IsDraftOrderCancelRequest("batal") {
		t.Fatal("standalone batal must cancel draft")
	}
}

func TestChaosSoftRegretWithStatus(t *testing.T) {
	msg := "maaf baru bales, saya ga jadi beli ya kok. apa sudah dibuatkan nomor pesanan untuk saya ?"
	if ShouldCancelPersistedOrder(msg) {
		t.Fatal("soft regret + status inquiry must not cancel persisted order")
	}
}
