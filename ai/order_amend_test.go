package ai

import (
	"strings"
	"testing"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/order"
)

func TestMergeOrderItemLines(t *testing.T) {
	existing := []order.OrderItem{
		{CatalogItemID: "maggi-percik", Name: "Maggi Percik", Qty: 1, UnitPrice: 70000},
	}
	added := []bf.OrderLineState{
		{CatalogItemID: "abon-250", ProductName: "Abon 250", Qty: 1, UnitPrice: 20000},
		{CatalogItemID: "nutella", ProductName: "Nutella", Qty: 1, UnitPrice: 155000},
	}
	merged := mergeOrderItemLines(existing, added)
	if len(merged) != 3 {
		t.Fatalf("want 3 items, got %d", len(merged))
	}
}

func TestIsOrderAmendMessageBridge(t *testing.T) {
	if !IsOrderAmendMessage("jadikan 1 dengan pesanan sebelumnya") {
		t.Fatal("expected amend")
	}
}

func TestOrderAmend_crossContactDenied(t *testing.T) {
	draft := &persistedOrder{
		ContactID:      "contact-a",
		ConversationID: "convo-1",
		Status:         "draft",
	}
	if OrderAccessibleByContact(draft, "contact-b", "convo-1") {
		t.Fatal("contact B must not amend draft owned by contact A")
	}
	if !OrderAccessibleByContact(draft, "contact-a", "convo-1") {
		t.Fatal("owner contact should access own draft")
	}
}

func TestOrderAmend_nonDraftRejected(t *testing.T) {
	reply := orderAmendNonDraftReply(false)
	if reply == "" {
		t.Fatal("expected non-draft reply")
	}
	if strings.Contains(strings.ToLower(reply), "katalog") {
		t.Fatalf("non-draft reply must not fall back to catalog: %s", reply)
	}
}

func TestOrderAmend_idempotent(t *testing.T) {
	key := amendIdempotencyKey + "tenant-1:inbound-msg-99"
	if !strings.HasPrefix(key, "ai:order-amend:") {
		t.Fatalf("unexpected idempotency key prefix: %s", key)
	}
	existing := []order.OrderItem{
		{CatalogItemID: "abon-250", Name: "Abon 250", Qty: 1, UnitPrice: 20000},
	}
	added := []bf.OrderLineState{
		{CatalogItemID: "abon-250", ProductName: "Abon 250", Qty: 1, UnitPrice: 20000},
	}
	merged := mergeOrderItemLines(existing, added)
	if len(merged) != 1 || merged[0].Qty != 2 {
		t.Fatalf("duplicate amend lines should sum qty, got %+v", merged)
	}
}
