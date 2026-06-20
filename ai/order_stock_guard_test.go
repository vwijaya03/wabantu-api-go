package ai

import (
	"strings"
	"testing"
)

func TestValidateOrderQtyAgainstStock_trackedOverQty(t *testing.T) {
	catalog := []dbCatalogItem{
		{ID: "item-1", Name: "Kaos Polos", StockTracked: true, StockAvailable: 2},
	}
	st := orderState{CatalogItemID: "item-1", ProductName: "Kaos Polos", Qty: 5}
	reject, reply := validateOrderQtyAgainstStock(st, catalog, false)
	if !reject {
		t.Fatal("expected reject when qty > available")
	}
	if !strings.Contains(reply, "cuma 2") {
		t.Fatalf("expected stock limit in reply, got: %s", reply)
	}
}

func TestValidateOrderQtyAgainstStock_untrackedPasses(t *testing.T) {
	catalog := []dbCatalogItem{
		{ID: "item-1", Name: "Kaos Polos", StockTracked: false, StockAvailable: 0},
	}
	st := orderState{CatalogItemID: "item-1", ProductName: "Kaos Polos", Qty: 99}
	if reject, _ := validateOrderQtyAgainstStock(st, catalog, false); reject {
		t.Fatal("untracked item should not reject")
	}
}

func TestValidateOrderQtyAgainstStock_outOfStock(t *testing.T) {
	catalog := []dbCatalogItem{
		{ID: "item-1", Name: "Kaos Polos", StockTracked: true, StockAvailable: 0},
	}
	st := orderState{CatalogItemID: "item-1", ProductName: "Kaos Polos", Qty: 1}
	reject, reply := validateOrderQtyAgainstStock(st, catalog, false)
	if !reject {
		t.Fatal("expected reject when out of stock")
	}
	if !strings.Contains(reply, "habis") {
		t.Fatalf("expected habis in reply, got: %s", reply)
	}
}

func TestAdvanceOrderFlow_rejectsQtyOverStock(t *testing.T) {
	catalog := []dbCatalogItem{
		{
			ID: "jeans-1", Name: "Skinny Jeans", SellPrice: 150000, SellUnit: "pcs",
			StockTracked: true, StockAvailable: 2,
		},
	}
	state := &orderState{
		Step: "ask_qty", CatalogItemID: "jeans-1", ProductName: "Skinny Jeans",
		UnitPrice: 150000, SellUnit: "pcs", Size: "M",
	}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "5 pcs",
		State:    state,
		Catalog:  catalog,
		Profile:  &dbBusinessProfile{BusinessName: "Toko", Tone: strPtr("casual")},
	}, nil)
	if res.Completed {
		t.Fatal("order should not complete when qty exceeds stock")
	}
	if res.State == nil || res.State.Step != "ask_qty" {
		t.Fatalf("expected stay on ask_qty, got step=%v", res.State)
	}
	if !strings.Contains(res.Reply, "cuma 2") {
		t.Fatalf("expected stock reject reply, got: %s", res.Reply)
	}
}

func TestAdvanceOrderFlow_allowsQtyWithinStock(t *testing.T) {
	catalog := []dbCatalogItem{
		{
			ID: "jeans-1", Name: "Skinny Jeans", SellPrice: 150000, SellUnit: "pcs",
			StockTracked: true, StockAvailable: 5,
		},
	}
	state := &orderState{
		Step: "ask_qty", CatalogItemID: "jeans-1", ProductName: "Skinny Jeans",
		UnitPrice: 150000, SellUnit: "pcs", Size: "M",
	}
	res := AdvanceOrderFlow(OrderFlowInput{
		UserText: "2 pcs",
		State:    state,
		Catalog:  catalog,
		Profile:  &dbBusinessProfile{BusinessName: "Toko", Tone: strPtr("casual")},
	}, nil)
	if res.State == nil || res.State.Step != "ask_recipient" {
		t.Fatalf("expected advance to ask_recipient, got step=%v", res.State)
	}
}

func TestBuildCatalogItemReply_stockLabels(t *testing.T) {
	out := buildCatalogItemReply(false, &dbCatalogItem{
		Name: "Kaos", SellPrice: 50000, StockTracked: true, StockAvailable: 0,
	}, 0)
	if !strings.Contains(out, "habis") {
		t.Fatalf("expected habis label, got: %s", out)
	}
	out = buildCatalogItemReply(false, &dbCatalogItem{
		Name: "Kaos", SellPrice: 50000, StockTracked: true, StockAvailable: 12,
	}, 0)
	if !strings.Contains(out, "Stok tersedia: 12") {
		t.Fatalf("expected available stock line, got: %s", out)
	}
}
