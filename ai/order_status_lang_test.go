package ai

import (
	"strings"
	"testing"
)

// Status replies follow the buyer's locked language (ID default, EN when the buyer is English).

func statusTestOrder(status, payment string) *persistedOrder {
	return &persistedOrder{
		ID:             "eb76635c-8439-42f1-9a45-dfa31bc0bbf4",
		ContactID:      "c1",
		ConversationID: "v1",
		Status:         status,
		PaymentStatus:  payment,
		ItemsJSON:      []byte(`[{"name":"Salmon Steak 100 Gram","qty":2,"unitPrice":50000}]`),
		ShippingJSON:   []byte(`{"name":"Ana","city":"Surabaya"}`),
		Total:          100000,
	}
}

func statusTestScope() orderAccessScope {
	return orderAccessScope{ConversationID: "v1", ContactID: "c1"}
}

func TestOrderStatusReplyForIndonesianUnchanged(t *testing.T) {
	o := statusTestOrder("draft", "unpaid")
	text, id := orderStatusReplyFor(persistedOrderResolve{Order: o}, "status pesanan", false, statusTestScope(), nil, nil)
	for _, want := range []string{"Nomor pesanan: WB-EB76635C", "Status pesanan: menunggu konfirmasi toko", "Pembayaran: belum ada bukti transfer",
		"Produk:", "Penerima: Ana", "Kota: Surabaya", "Total: Rp100000", "Untuk membatalkan, ketik saja: batalkan pesanan."} {
		if !strings.Contains(text, want) {
			t.Errorf("ID reply lacks %q:\n%s", want, text)
		}
	}
	if id != o.ID {
		t.Errorf("order id = %q", id)
	}
}

func TestOrderStatusReplyForEnglish(t *testing.T) {
	o := statusTestOrder("draft", "unpaid")
	text, id := orderStatusReplyFor(persistedOrderResolve{Order: o}, "where is my order", true, statusTestScope(), nil, nil)
	for _, want := range []string{"Order number: WB-EB76635C", "Order status: awaiting shop confirmation", "Payment: no transfer proof yet",
		"Items:", "Recipient: Ana", "City: Surabaya", "Total: Rp100000", "To cancel, just type: cancel order."} {
		if !strings.Contains(text, want) {
			t.Errorf("EN reply lacks %q:\n%s", want, text)
		}
	}
	for _, bad := range []string{"Nomor pesanan", "Status pesanan", "Pembayaran", "Produk:", "Penerima", "Kota", "membatalkan"} {
		if strings.Contains(text, bad) {
			t.Errorf("EN reply still has Indonesian %q:\n%s", bad, text)
		}
	}
	if id != o.ID {
		t.Errorf("order id = %q", id)
	}
}

func TestOrderStatusReplyForEnglishStatusAndPaymentLabels(t *testing.T) {
	cases := []struct{ status, payment, wantStatus, wantPay string }{
		{"processing", "proof_submitted", "being processed", "transfer proof needs checking"},
		{"confirmed", "verified", "confirmed", "payment verified"},
		{"paid", "rejected", "confirmed", "transfer proof rejected"},
		{"shipped", "verified", "shipped", "payment verified"},
		{"completed", "verified", "completed", "payment verified"},
	}
	for _, c := range cases {
		text, _ := orderStatusReplyFor(persistedOrderResolve{Order: statusTestOrder(c.status, c.payment)}, "status", true, statusTestScope(), nil, nil)
		if !strings.Contains(text, "Order status: "+c.wantStatus) || !strings.Contains(text, "Payment: "+c.wantPay) {
			t.Errorf("%s/%s:\n%s", c.status, c.payment, text)
		}
	}
	cancelled, _ := orderStatusReplyFor(persistedOrderResolve{Order: statusTestOrder("cancelled", "unpaid")}, "status", true, statusTestScope(), nil, nil)
	if !strings.Contains(cancelled, "Order status: cancelled") || !strings.Contains(cancelled, "This order has been cancelled.") || strings.Contains(cancelled, "To cancel") {
		t.Errorf("cancelled:\n%s", cancelled)
	}
	idCancelled, _ := orderStatusReplyFor(persistedOrderResolve{Order: statusTestOrder("cancelled", "unpaid")}, "status", false, statusTestScope(), nil, nil)
	if !strings.Contains(idCancelled, "Pesanan ini sudah dibatalkan.") {
		t.Errorf("ID cancelled:\n%s", idCancelled)
	}
}

func TestOrderStatusReplyForEnglishRejectedProofDetail(t *testing.T) {
	o := statusTestOrder("draft", "rejected")
	o.PaymentProofMetaJSON = []byte(`{"flags":["mismatch_amount"]}`)
	text, _ := orderStatusReplyFor(persistedOrderResolve{Order: o}, "status", true, statusTestScope(), nil, nil)
	if !strings.Contains(text, "Reason: the transfer amount does not match the order total.") {
		t.Errorf("EN proof detail:\n%s", text)
	}
	o.PaymentProofMetaJSON = []byte(`{"rejectionCount":0}`)
	text, _ = orderStatusReplyFor(persistedOrderResolve{Order: o}, "status", true, statusTestScope(), nil, nil)
	if !strings.Contains(text, "Please resend a transfer proof") {
		t.Errorf("EN default proof detail:\n%s", text)
	}
}

func TestOrderStatusReplyForEnglishNoOrderBranches(t *testing.T) {
	sc := statusTestScope()
	cases := []struct {
		name string
		res  persistedOrderResolve
		text string
		want string
	}{
		{"denied", persistedOrderResolve{AccessDenied: true}, "status WB-AAAA1111", "That order is not registered to this WhatsApp number"},
		{"notfound ref", persistedOrderResolve{NotFound: true}, "status WB-AAAA1111", "Order number WB-AAAA1111 was not found in this chat."},
		{"notfound blank", persistedOrderResolve{NotFound: true}, "status", "Order number was not found in this chat."},
		{"hint", persistedOrderResolve{RecipientHintNotFound: true}, "status", "No order under that name in this chat"},
		{"active only", persistedOrderResolve{ActiveOnly: true}, "status", "There are no active orders from this chat right now."},
		{"none", persistedOrderResolve{}, "status", "No orders have been recorded from this chat yet."},
	}
	for _, c := range cases {
		got, _ := orderStatusReplyFor(c.res, c.text, true, sc, nil, nil)
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: %q lacks %q", c.name, got, c.want)
		}
		id, _ := orderStatusReplyFor(c.res, c.text, false, sc, nil, nil)
		if strings.Contains(id, c.want) || id == got {
			t.Errorf("%s: Indonesian reply must differ: %q", c.name, id)
		}
	}
}

func TestOrderStatusReplyForEnglishPickList(t *testing.T) {
	list := []persistedOrder{
		*statusTestOrder("draft", "unpaid"),
		{ID: "eaa94534-1758-4cbe-830c-b2ba16244b0c", Status: "processing", PaymentStatus: "verified", ItemsJSON: []byte(`[{"name":"Hello Kitty"}]`)},
	}
	text, _ := orderStatusReplyFor(persistedOrderResolve{NeedPick: true, List: list}, "status", true, statusTestScope(), nil, nil)
	for _, want := range []string{"There are several orders in this chat:", "1. WB-EB76635C — Salmon Steak 100 Gram (awaiting shop confirmation · no transfer proof yet)",
		"2. WB-EAA94534 — Hello Kitty (being processed · payment verified)", "Give the order number (e.g. WB-A1B2C3D4)"} {
		if !strings.Contains(text, want) {
			t.Errorf("EN pick list lacks %q:\n%s", want, text)
		}
	}
	idText, _ := orderStatusReplyFor(persistedOrderResolve{NeedPick: true, List: list}, "status", false, statusTestScope(), nil, nil)
	if !strings.Contains(idText, "Dari chat ini ada beberapa pesanan:") || !strings.Contains(idText, "(menunggu konfirmasi toko · belum ada bukti transfer)") {
		t.Errorf("ID pick list changed:\n%s", idText)
	}
}

func TestOrderStatusReplyForChatDraftNote(t *testing.T) {
	draft := func() string { return "Order Summary\n\nProduct: tuna" }
	text, id := orderStatusReplyFor(persistedOrderResolve{}, "status", true, statusTestScope(), nil, draft)
	if !strings.HasSuffix(text, "(This is a draft order from the chat — not yet confirmed with the shop.)") || id != "" {
		t.Errorf("EN draft note: %q", text)
	}
	text, _ = orderStatusReplyFor(persistedOrderResolve{}, "status", false, statusTestScope(), nil, draft)
	if !strings.HasSuffix(text, "(Ini draft pesanan dari chat — belum dikonfirmasi ke toko.)") {
		t.Errorf("ID draft note: %q", text)
	}
}

func TestOrderStatusReplyForForeignOrderIsDenied(t *testing.T) {
	o := statusTestOrder("draft", "unpaid")
	o.ContactID = "someone-else"
	contact := &dbContact{PhoneNumber: "628111"}
	text, _ := orderStatusReplyFor(persistedOrderResolve{Order: o}, "status", true, statusTestScope(), contact, nil)
	if !strings.Contains(text, "not registered to this WhatsApp number") {
		t.Errorf("foreign order must be denied in EN: %q", text)
	}
}

func TestBuyerEnglishForStatusFollowsLockAndMessage(t *testing.T) {
	en, id := "en", "id"
	if !buyerEnglish("where is my order", &dbContact{PreferredLanguage: &en}) {
		t.Error("locked EN")
	}
	if buyerEnglish("status pesanan", &dbContact{PreferredLanguage: &id}) {
		t.Error("locked ID")
	}
	if !buyerEnglish("please reply in English, status WB-AAAA1111", &dbContact{PreferredLanguage: &id}) {
		t.Error("explicit switch beats the lock")
	}
	if buyerEnglish("status", nil) {
		t.Error("default is Indonesian")
	}
}
