package ai

import (
	"strings"
	"testing"
)

func TestTryPaymentFAQAnswer(t *testing.T) {
	cat := "Nomor Rekening"
	kb := []dbKBEntry{{
		Question: "Nomor Rekening",
		Answer:   "BCA 110220330 atas nama Omah Apparel",
		Category: &cat,
		IsActive: true,
	}}
	ans, ok := tryPaymentFAQAnswer("bisa minta nomor rekeningnya ga sih ?", kb)
	if !ok {
		t.Fatal("expected payment FAQ match")
	}
	if !strings.Contains(strings.ToUpper(ans), "BCA") {
		t.Fatalf("unexpected answer: %q", ans)
	}
	_, ok = tryPaymentFAQAnswer("best seller apa?", kb)
	if ok {
		t.Fatal("non-payment question should not match payment FAQ")
	}
}

func TestIsOrderRefStatusLookup(t *testing.T) {
	if !IsOrderRefStatusLookup("WB-58D662BC") {
		t.Fatal("bare order ref should trigger status lookup")
	}
	if !IsOrderRefStatusLookup("mau lihat detail pesanan WB-58D662BC") {
		t.Fatal("detail pesanan with ref should trigger status lookup")
	}
	if IsOrderRefStatusLookup("halo kak") {
		t.Fatal("greeting should not trigger order ref lookup")
	}
}

func TestIsPaymentProofInboundResubmit(t *testing.T) {
	if !IsPaymentProofInbound("image", "coba cek lagi min, ini saya kirim lagi") {
		t.Fatal("resubmit caption should be payment proof inbound")
	}
}
