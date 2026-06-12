package ai

import (
	"fmt"
	"testing"
)

func TestBuyerBehavior1000(t *testing.T) {
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

func TestBuyerBehaviorScriptHappyAbon(t *testing.T) {
	sim := newOmahSimulator()
	o := sim.RunScript(
		"saya jadi beli abon sapi 2 pcs",
		recipientBlock("Budi", "081234567890"),
		fullAddressBlock(),
	)
	if !o[len(o)-1].Completed {
		t.Fatalf("expected complete, last=%+v", o[len(o)-1])
	}
}

func TestBuyerBehaviorRevisionNotCancel(t *testing.T) {
	sim := newOmahSimulator()
	sim.RunScript("saya jadi beli abon sapi 1 pcs")
	last := sim.Turn("ga jadi mau dirubah menjadi 10 biji ya")
	if last.Canceled {
		t.Fatal("revision should not cancel")
	}
	if last.Order == nil || last.Order.Qty != 10 {
		t.Fatalf("expected qty 10, order=%+v", last.Order)
	}
}
