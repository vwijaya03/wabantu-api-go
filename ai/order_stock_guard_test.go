package ai

import (
	"strings"
	"testing"
)

func stockLines(defaultAvail, branchAvail float64) []catalogStockLine {
	return []catalogStockLine{
		{WarehouseID: "wh-default", WarehouseName: "Gudang Utama", IsDefault: true, Available: defaultAvail},
		{WarehouseID: "wh-branch", WarehouseName: "GD-SBY-INTERNAL", CustomerLabel: "Surabaya", Available: branchAvail},
	}
}

func TestWarehouseBuyerLabel(t *testing.T) {
	if got := warehouseBuyerLabel("Surabaya", "GD-SBY"); got != "Surabaya" {
		t.Fatalf("customer label = %q", got)
	}
	if got := warehouseBuyerLabel("", "Gudang Utama"); got != "Gudang Utama" {
		t.Fatalf("fallback name = %q", got)
	}
}

func TestValidateOrderQtyAgainstStock_singleWarehouseMax(t *testing.T) {
	catalog := []dbCatalogItem{
		{
			ID: "item-1", Name: "Kaos Polos", StockTracked: true, StockAvailable: 5,
			StockByWarehouse: stockLines(2, 3),
		},
	}
	st := orderState{CatalogItemID: "item-1", ProductName: "Kaos Polos", Qty: 5}
	reject, reply, wh := validateOrderQtyAgainstStock(st, catalog, false)
	if !reject {
		t.Fatal("expected reject when no single warehouse has 5")
	}
	if !strings.Contains(reply, "maksimal 3") {
		t.Fatalf("expected max single warehouse in reply, got: %s", reply)
	}
	if wh != "" {
		t.Fatalf("warehouse should be empty on reject, got %q", wh)
	}
}

func TestValidateOrderQtyAgainstStock_assignsDefaultWarehouse(t *testing.T) {
	catalog := []dbCatalogItem{
		{
			ID: "item-1", Name: "Kaos Polos", StockTracked: true, StockAvailable: 5,
			StockByWarehouse: stockLines(5, 3),
		},
	}
	st := orderState{CatalogItemID: "item-1", ProductName: "Kaos Polos", Qty: 3}
	reject, _, wh := validateOrderQtyAgainstStock(st, catalog, false)
	if reject {
		t.Fatal("expected allow when default warehouse has enough")
	}
	if wh != "wh-default" {
		t.Fatalf("warehouse = %q, want wh-default", wh)
	}
}

func TestValidateOrderQtyAgainstStock_untrackedPasses(t *testing.T) {
	catalog := []dbCatalogItem{
		{ID: "item-1", Name: "Kaos Polos", StockTracked: false},
	}
	st := orderState{CatalogItemID: "item-1", ProductName: "Kaos Polos", Qty: 99}
	if reject, _, _ := validateOrderQtyAgainstStock(st, catalog, false); reject {
		t.Fatal("untracked item should not reject")
	}
}

func TestAdvanceOrderFlow_rejectsQtyOverSingleWarehouse(t *testing.T) {
	catalog := []dbCatalogItem{
		{
			ID: "jeans-1", Name: "Skinny Jeans", SellPrice: 150000, SellUnit: "pcs",
			StockTracked: true, StockAvailable: 5,
			StockByWarehouse: stockLines(2, 3),
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
		t.Fatal("order should not complete when qty exceeds any single warehouse")
	}
	if res.State == nil || res.State.Step != "ask_qty" {
		t.Fatalf("expected stay on ask_qty, got step=%v", res.State)
	}
	if !strings.Contains(res.Reply, "per gudang") {
		t.Fatalf("expected per-warehouse reply, got: %s", res.Reply)
	}
}

func TestAdvanceOrderFlow_allowsQtyWithinDefaultWarehouse(t *testing.T) {
	catalog := []dbCatalogItem{
		{
			ID: "jeans-1", Name: "Skinny Jeans", SellPrice: 150000, SellUnit: "pcs",
			StockTracked: true, StockAvailable: 5,
			StockByWarehouse: stockLines(5, 3),
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
	if res.State.WarehouseID != "wh-default" {
		t.Fatalf("warehouse = %q, want wh-default", res.State.WarehouseID)
	}
}

func TestBuildCatalogItemReply_stockBreakdown(t *testing.T) {
	out := buildCatalogItemReply(false, &dbCatalogItem{
		Name: "Kaos", SellPrice: 50000, StockTracked: true, StockAvailable: 5,
		StockByWarehouse: stockLines(2, 3),
	}, 0)
	if !strings.Contains(out, "Gudang Utama: 2") {
		t.Fatalf("expected default warehouse name, got: %s", out)
	}
	if !strings.Contains(out, "Surabaya: 3") {
		t.Fatalf("expected customer label, got: %s", out)
	}
	if !strings.Contains(out, "Total: 5") {
		t.Fatalf("expected total line, got: %s", out)
	}
}

func TestBuildCatalogItemReply_usesWarehouseNameWhenNoCustomerLabel(t *testing.T) {
	out := buildCatalogItemReply(false, &dbCatalogItem{
		Name: "Kaos", SellPrice: 50000, StockTracked: true, StockAvailable: 2,
		StockByWarehouse: []catalogStockLine{
			{WarehouseName: "Gudang Omah Belakang", Available: 2},
		},
	}, 0)
	if !strings.Contains(out, "Gudang Omah Belakang: 2") {
		t.Fatalf("expected warehouse name as-is, got: %s", out)
	}
}

func TestBuildCatalogItemReply_stockHabis(t *testing.T) {
	out := buildCatalogItemReply(false, &dbCatalogItem{
		Name: "Kaos", SellPrice: 50000, StockTracked: true, StockAvailable: 0,
	}, 0)
	if !strings.Contains(out, "habis") {
		t.Fatalf("expected habis label, got: %s", out)
	}
}
