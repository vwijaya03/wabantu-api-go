package ai

import "testing"

func TestIsOrderContinuationMessagePcs(t *testing.T) {
	if !IsOrderContinuationMessage("1 pcs saja") {
		t.Fatal("expected qty continuation")
	}
}

func TestParseOrderHintsFullLine(t *testing.T) {
	h := parseOrderHints("mau pesan skinny jeans merk jiniso ukuran XL bisa ? dengan qty 1")
	if !h.HasSize || h.Variant == "" {
		t.Fatalf("expected size, got %+v", h)
	}
	if !h.HasQty || h.Qty != 1 {
		t.Fatalf("expected qty 1, got %+v", h)
	}
}

func TestIsWithinBusinessScopePcsOnly(t *testing.T) {
	scope := ExtractScopeKeywords("Omah Apparel jeans skinny")
	if !IsWithinBusinessScope("1 pcs saja", scope, nil) {
		t.Fatal("pcs-only reply should be in scope during order")
	}
}
