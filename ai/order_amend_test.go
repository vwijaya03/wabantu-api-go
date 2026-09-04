package ai

import (
	"encoding/json"
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

func TestShippingAddressJSONIsComplete(t *testing.T) {
	placeholder, _ := json.Marshal(order.ShippingAddress{Country: "Indonesia"})
	if shippingAddressJSONIsComplete(placeholder) {
		t.Fatal("placeholder Country-only address must not overwrite existing shipping")
	}
	if shippingAddressJSONIsComplete(nil) {
		t.Fatal("empty JSON is incomplete")
	}
	complete, _ := json.Marshal(order.ShippingAddress{
		Name: "Budi", Phone: "08123456789", Street: "Jl Melati 1", City: "Jakarta", PostalCode: "12345",
	})
	if !shippingAddressJSONIsComplete(complete) {
		t.Fatal("name+phone+street should be complete")
	}
}

func TestDecideDraftWrite(t *testing.T) {
	if got := decideDraftWrite(true, false, 2); got != draftWriteInsert {
		t.Fatalf("pesanan baru with leftover drafts must INSERT, got %d", got)
	}
	if got := decideDraftWrite(false, true, 3); got != draftWriteUpdatePinned {
		t.Fatalf("pinned draft must UPDATE, got %d", got)
	}
	if got := decideDraftWrite(false, false, 1); got != draftWriteNeedPick {
		t.Fatalf("single leftover draft without pin must pick, not clobber, got %d", got)
	}
	if got := decideDraftWrite(false, false, 0); got != draftWriteInsert {
		t.Fatalf("no leftover drafts must INSERT, got %d", got)
	}
}

func TestOrderStateFromPersistedDraftHydratesCart(t *testing.T) {
	items, err := json.Marshal([]order.OrderItem{
		{CatalogItemID: "maggi-percik", Name: "Maggi Percik", Qty: 2, UnitPrice: 70000, SellUnit: "pcs"},
		{CatalogItemID: "abon-250", Name: "Abon 250", Qty: 1, UnitPrice: 20000},
	})
	if err != nil {
		t.Fatal(err)
	}
	ship, err := json.Marshal(order.ShippingAddress{
		Name: "Sari", Phone: "08111111111", Street: "Jl Kenanga", City: "Bandung", Province: "Jawa Barat", PostalCode: "40111", Country: "Indonesia",
	})
	if err != nil {
		t.Fatal(err)
	}
	st := orderStateFromPersistedDraft(&persistedOrder{
		ID:           "draft-1",
		ItemsJSON:    items,
		ShippingJSON: ship,
	})
	if st.PersistedOrderID != "draft-1" {
		t.Fatalf("pin: %s", st.PersistedOrderID)
	}
	if st.Step != "ask_recipient" {
		t.Fatalf("step: %s", st.Step)
	}
	if len(st.Items) != 2 {
		t.Fatalf("want 2 hydrated lines, got %d", len(st.Items))
	}
	if st.CatalogItemID != "maggi-percik" || st.Qty != 2 {
		t.Fatalf("first line not applied: catalog=%s qty=%d", st.CatalogItemID, st.Qty)
	}
	if st.RecipientName != "Sari" || st.RecipientPhone != "08111111111" || st.City != "Bandung" {
		t.Fatalf("shipping not hydrated: %+v", st)
	}
	if strings.TrimSpace(st.ProductName) == "" {
		t.Fatal("product name should hydrate from first line")
	}
}
