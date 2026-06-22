package ai

import (
	"strings"
	"testing"
)

func lolBestSellerCatalog() []dbCatalogItem {
	return []dbCatalogItem{{
		ID:           "lol-1",
		ExternalCode: "LOL-BS",
		Name:         "LOL Best Seller",
		SellPrice:    50000,
		SellUnit:     "pcs",
	}}
}

func TestIsStructuredOrderList_transcript(t *testing.T) {
	msg := `mau buat pesanan baru
barang yang dibeli
1. LOL Best Seller 1 lusin ya ukuran L
2. LOL Best Seller 1 lusin ya ukuran XL
3. LOL Best Seller 1 lusin ya ukuran XXL`
	if !IsStructuredOrderList(msg) {
		t.Fatal("expected structured order list")
	}
}

func TestParseStructuredOrderLines_lusinAndSize(t *testing.T) {
	catalog := lolBestSellerCatalog()
	msg := `mau buat pesanan baru
1. LOL Best Seller 1 lusin ya ukuran L`
	parsed := parseStructuredOrderLines(msg, catalog)
	if len(parsed.Lines) != 1 {
		t.Fatalf("want 1 line, got %d unmatched=%v", len(parsed.Lines), parsed.Unmatched)
	}
	ln := parsed.Lines[0]
	if ln.Qty != 12 {
		t.Fatalf("qty want 12 (1 lusin), got %d", ln.Qty)
	}
	if ln.Size != "L" {
		t.Fatalf("size want L, got %q", ln.Size)
	}
	if ln.ProductName != "LOL Best Seller" {
		t.Fatalf("product want LOL Best Seller, got %q", ln.ProductName)
	}
}

func TestParseStructuredOrderLines_threeLines(t *testing.T) {
	catalog := lolBestSellerCatalog()
	msg := `mau buat pesanan baru
barang yang dibeli
1. LOL Best Seller 1 lusin ya ukuran L
2. LOL Best Seller 1 lusin ya ukuran XL
3. LOL Best Seller 1 lusin ya ukuran XXL`
	parsed := parseStructuredOrderLines(msg, catalog)
	if len(parsed.Lines) != 3 {
		t.Fatalf("want 3 lines, got %d unmatched=%v", len(parsed.Lines), parsed.Unmatched)
	}
	wantSizes := []string{"L", "XL", "XXL"}
	for i, ln := range parsed.Lines {
		if ln.Qty != 12 {
			t.Fatalf("line %d qty want 12, got %d", i+1, ln.Qty)
		}
		if ln.Size != wantSizes[i] {
			t.Fatalf("line %d size want %s, got %q", i+1, wantSizes[i], ln.Size)
		}
	}
}

func TestFormatMultiOrderSummary(t *testing.T) {
	st := orderStateFromStructuredLines([]orderLineState{
		{ProductName: "LOL Best Seller", Qty: 12, Size: "L", UnitPrice: 50000, SellUnit: "pcs"},
		{ProductName: "LOL Best Seller", Qty: 12, Size: "XL", UnitPrice: 50000, SellUnit: "pcs"},
	})
	st.Items[0].CatalogItemID = "lol-1"
	st.Items[1].CatalogItemID = "lol-1"
	summary := formatOrderSummary(st)
	if !strings.Contains(summary, "LOL Best Seller") {
		t.Fatalf("missing product: %s", summary)
	}
	if !strings.Contains(summary, "1. ") || !strings.Contains(summary, "2. ") {
		t.Fatalf("expected numbered lines: %s", summary)
	}
	if !strings.Contains(summary, "Subtotal") {
		t.Fatalf("expected subtotal: %s", summary)
	}
}

func TestResolveSalesIntent_structuredOrderList(t *testing.T) {
	profile := &dbBusinessProfile{BusinessName: "Toko"}
	catalog := lolBestSellerCatalog()
	msg := `mau buat pesanan baru
1. LOL Best Seller 1 lusin ya ukuran L`
	intent := ResolveSalesIntent(msg, nil, false, true, profile, catalog)
	if intent.State != SalesStateCartReady {
		t.Fatalf("want cart_ready, got %+v", intent)
	}
}
