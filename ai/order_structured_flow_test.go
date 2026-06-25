package ai

import (
	"strings"
	"testing"
)

func omahHelloKittyTranscriptMsg() string {
	return `mau beli beberapa barang ini ya

CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY BUNGA LEMBUT - L 1 lusin
CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY BUNGA LEMBUT - XL 2 lusin
CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY BUNGA LEMBUT - XXL 3 lusin`
}

func TestShouldBreakOrderFlow_structuredOrderList(t *testing.T) {
	msg := omahHelloKittyTranscriptMsg()
	if !ShouldBreakOrderFlow(msg, "ask_qty", omahCatalog()) {
		t.Fatal("structured multi-line order should break stale ask_qty flow")
	}
}

func TestHistoryBackedPurchase_skipsWhenStructuredList(t *testing.T) {
	catalog := append(abonCatalog(), helloKittyCatalog()...)
	history := []dbMessage{{
		Direction: "out",
		Body:      "Maaf kak, stok Abon Sapi 500G per gudang:\n• Gudang A: 8\nPesanan 36 pcs belum bisa",
	}}
	msg := omahHelloKittyTranscriptMsg()
	if isHistoryBackedPurchaseIntent(msg, history, catalog) {
		t.Fatal("structured list with named products should not use history-backed purchase")
	}
}

func TestEvaluateStructuredOrder_omahHelloKittyTranscript(t *testing.T) {
	catalog := helloKittyCatalog()
	out := evaluateStructuredOrder(omahHelloKittyTranscriptMsg(), catalog, false)
	if !out.Matched || len(out.Lines) != 3 {
		t.Fatalf("want 3 lines, got matched=%v lines=%d unmatched=%v", out.Matched, len(out.Lines), out.Unmatched)
	}
	if out.Blocked {
		t.Fatalf("hello kitty lines should not stock-block in fixture catalog: %s", out.BlockReply)
	}
	if out.State.Step != "ask_recipient" {
		t.Fatalf("want ask_recipient, got %s", out.State.Step)
	}
	if strings.Contains(out.State.ProductName, "Abon") {
		t.Fatalf("should not bind Abon for hello kitty transcript: %q", out.State.ProductName)
	}
}

func TestIsUserSalesCorrection_bukanApiSapi(t *testing.T) {
	msg := omahHelloKittyTranscriptMsg() + "\n\nbukan api sapi loh"
	if !IsUserSalesCorrection(msg) {
		t.Fatal("expected product denial correction")
	}
}
