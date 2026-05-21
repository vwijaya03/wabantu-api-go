package ai

import "testing"

func TestPaymentTransferInScope(t *testing.T) {
	scope := ExtractScopeKeywords("Omah Apparel jeans highwaist")
	if !IsWithinBusinessScope("nanti saya transfer", scope, nil) {
		t.Fatal("payment transfer should be in business scope")
	}
	if IsOffBusinessProductRequest("nanti saya transfer", scope) {
		t.Fatal("transfer must not be treated as off-topic product (travel substring)")
	}
}

func TestActiveCheckoutFromHistoryAfterOrderComplete(t *testing.T) {
	hist := []dbMessage{
		{Direction: "in", Body: "kirim ke Jl Taman Setiabudi"},
		{Direction: "out", Body: "Sip kak, datanya sudah lengkap. Tim CS kami akan segera konfirmasi order kakak ya"},
		{Direction: "in", Body: "ongkir kena berapa ?"},
		{Direction: "out", Body: "Tim kami akan menghubungi untuk biaya pengiriman"},
	}
	if !IsActiveCheckoutFromHistory(hist, "nanti saya trf") {
		t.Fatal("expected post-order payment follow-up from history")
	}
}
