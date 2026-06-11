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

func TestParseOrderQtyIgnoresCatalogPrefix(t *testing.T) {
	msg := "1PCS CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY BUNGA LEMBUT - L\n\n2 biji"
	qty, ok := parseOrderQty(msg)
	if !ok || qty != 2 {
		t.Fatalf("expected qty 2, got %d ok=%v", qty, ok)
	}
}

func TestParseOrderQtyPieceUnit(t *testing.T) {
	for _, msg := range []string{
		"mau order\n1PCS CELANA DALAM - L\n\n2 piece bisa?",
		"mau beli 2 piece ya bukan 1",
	} {
		qty, ok := parseOrderQty(msg)
		if !ok || qty != 2 {
			t.Fatalf("expected qty 2 for %q, got %d ok=%v", msg, qty, ok)
		}
	}
}

func TestParseOrderQtyDoesNotMatchGluedPCS(t *testing.T) {
	if mentionsOrderQty("1PCS CELANA DALAM BOXER") {
		t.Fatal("glued 1PCS product title should not count as order qty")
	}
}

func TestIsWithinBusinessScopePcsOnly(t *testing.T) {
	scope := ExtractScopeKeywords("Omah Apparel jeans skinny")
	if !IsWithinBusinessScope("1 pcs saja", scope, nil) {
		t.Fatal("pcs-only reply should be in scope during order")
	}
}

func TestIsStoreLocationQuestion(t *testing.T) {
	msgs := []string{
		"ini tokonya dimananya ?",
		"tokonya di kota mana ini",
		"alamat toko dimana",
	}
	for _, m := range msgs {
		if !IsStoreLocationQuestion(m) {
			t.Fatalf("expected store location question: %q", m)
		}
		if !ShouldBreakOrderFlow(m, "ask_product") {
			t.Fatalf("should break order flow: %q", m)
		}
	}
}

func TestIsShippingQuoteNotOrderFollowUp(t *testing.T) {
	msg := "lalu minta tolong hitungkan ongkir ke jakarta"
	if !IsShippingQuoteQuestion(msg) {
		t.Fatal("expected shipping quote question")
	}
	hist := []dbMessage{
		{Direction: "out", Body: "Silakan konfirmasi alamat pengiriman agar kami hitung total"},
	}
	if IsOrderFollowUpFromHistory(hist, msg) {
		t.Fatal("ongkir quote should not be treated as order FSM continuation")
	}
}

func TestAddressJlWithoutDot(t *testing.T) {
	if !orderAddrHintRe.MatchString("Jl Taman Setiabudi II no 28") {
		t.Fatal("Jl without period should match address hint")
	}
}

func TestAcknowledgmentInScope(t *testing.T) {
	if !IsAcknowledgmentLike("oke terima kasih") {
		t.Fatal("expected acknowledgment")
	}
	scope := ExtractScopeKeywords("Omah Apparel jeans")
	if !IsWithinBusinessScope("oke terima kasih", scope, nil) {
		t.Fatal("thanks should stay in business scope")
	}
}
