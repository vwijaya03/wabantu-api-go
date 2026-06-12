package ai

import (
	"strings"
	"testing"
)

func TestFormatCatalogListBodyNoSKU(t *testing.T) {
	catalog := []dbCatalogItem{
		{ID: "1", ExternalCode: "HELLO-KITTY-BUNGA-L", Name: "1PCS CELANA DALAM BOXER HELLO KITTY - L", SellPrice: 21500, SellUnit: "pcs"},
		{ID: "2", ExternalCode: "HELLO-KITTY-BUNGA-XL", Name: "1PCS CELANA DALAM BOXER HELLO KITTY - XL", SellPrice: 21500, SellUnit: "pcs"},
		{ID: "3", ExternalCode: "LOL-L", Name: "1PCS CELANA DALAM BOXER LOL - L", SellPrice: 21500, SellUnit: "pcs"},
	}
	body := formatCatalogListBody(catalog, 8)
	if !strings.Contains(body, "🔥 Produk Pilihan") {
		t.Fatalf("expected featured format, got:\n%s", body)
	}
	if strings.Contains(body, "[HELLO") || strings.Contains(body, "HELLO-KITTY") {
		t.Fatalf("should not expose SKU in customer list:\n%s", body)
	}
}

func TestFormatOrderSummary(t *testing.T) {
	st := orderState{
		ProductName: "Celana Boxer - L",
		Qty:         2,
		UnitPrice:   21500,
		SellUnit:    "pcs",
		Size:        "L",
	}
	msg := formatOrderSummary(st)
	if !strings.Contains(msg, "Ringkasan Pesanan") || !strings.Contains(msg, "Qty: 2") {
		t.Fatalf("unexpected summary: %s", msg)
	}
	if !strings.Contains(msg, "43000") {
		t.Fatalf("expected subtotal 43000 in: %s", msg)
	}
}

func TestIsOrderTotalRequest(t *testing.T) {
	if !IsOrderTotalRequest("minta totalannya juga ya") {
		t.Fatal("expected total request")
	}
}

func TestParseOrderQtyLusin(t *testing.T) {
	qty, ok := parseOrderQty("mau beli abon sapi 1 lusin")
	if !ok || qty != 12 {
		t.Fatalf("expected 12 for 1 lusin, got %d ok=%v", qty, ok)
	}
}

func TestCatalogItemNeedsVariant(t *testing.T) {
	food := &dbCatalogItem{Name: "Abon Sapi 500G"}
	apparel := &dbCatalogItem{Name: "1PCS CELANA DALAM BOXER - L"}
	if catalogItemNeedsVariant(food) {
		t.Fatal("abon should not need variant")
	}
	if !catalogItemNeedsVariant(apparel) {
		t.Fatal("boxer should need variant")
	}
}

func TestBuildCatalogItemReplyWithQty(t *testing.T) {
	it := &dbCatalogItem{Name: "Jeans - XL", SellPrice: 150000, SellUnit: "pcs"}
	reply := buildCatalogItemReply(false, it, 2)
	if !strings.Contains(reply, "Subtotal") || !strings.Contains(reply, "300000") {
		t.Fatalf("expected subtotal in reply: %s", reply)
	}
	if strings.Contains(reply, "kode") {
		t.Fatal("should not show SKU/kode to customer")
	}
}
