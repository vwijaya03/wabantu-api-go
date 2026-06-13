package ai

import (
	"fmt"
	"testing"
)

func TestOrderOwnership100(t *testing.T) {
	cases := allOrderOwnershipCases()
	if len(cases) != 100 {
		t.Fatalf("expected 100 cases, got %d", len(cases))
	}
	stats := map[string]int{}
	for _, c := range cases {
		stats[c.Category]++
		name := fmt.Sprintf("%03d_%s_%s", c.ID, c.Category, c.Name)
		t.Run(name, func(t *testing.T) {
			if err := c.Pure(); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Logf("categories: %v", stats)
}

func TestProductionCrossOwnerCancelDenied(t *testing.T) {
	o := &persistedOrder{
		ID:             "eaa94534-1758-4cbe-830c-b2ba16244b0c",
		ContactID:      "contact-ngiek",
		ConversationID: "7f3f02c4-6a0a-4e01-912f-0be70dde81ca",
	}
	scope := orderAccessScope{
		ConversationID: o.ConversationID,
		ContactID:      "contact-local-test",
	}
	if OrderAccessibleByContact(o, scope.ContactID, scope.ConversationID) {
		t.Fatal("local test must not cancel The Ngiek Ing order WB-EAA94534")
	}
	if !OrderAccessibleByContact(o, "contact-ngiek", o.ConversationID) {
		t.Fatal("owner must access own order")
	}
}

func TestOrderAccessScopeValid(t *testing.T) {
	if (&orderAccessScope{ConversationID: "c", ContactID: "x"}).valid() != true {
		t.Fatal("valid scope")
	}
	if (&orderAccessScope{ConversationID: "", ContactID: "x"}).valid() != false {
		t.Fatal("invalid without convo")
	}
}
