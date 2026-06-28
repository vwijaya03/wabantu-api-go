package ai

import (
	"strings"
	"testing"
)

func TestFormatOrderNumber(t *testing.T) {
	got := FormatOrderNumber("eb76635c-8439-42f1-9a45-dfa31bc0bbf4")
	want := "WB-EB76635C"
	if got != want {
		t.Fatalf("FormatOrderNumber = %q, want %q", got, want)
	}
	if FormatOrderNumber("") != "" {
		t.Fatal("empty id should return empty ref")
	}
}

func TestIsOrderCancelRequest(t *testing.T) {
	draftCases := []struct {
		text string
		want bool
	}{
		{"mau saya batalkan ya", true},
		{"batalkan pesanan", true},
		{"batal", true},
		{"cancel", true},
		{"batalkan", true},
		{"ga jadi mau dirubah menjadi 10 biji ya", false},
		{"loh ubah jadi 10 paket", false},
		{"pesanan yang atas nama saya ada kah ?", false},
		{"harga berapa", false},
	}
	for _, tc := range draftCases {
		if got := IsDraftOrderCancelRequest(tc.text); got != tc.want {
			t.Fatalf("IsDraftOrderCancelRequest(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"tidak jadi order", true},
		{"ga jadi deh", true},
	} {
		if got := IsOrderCancelRequest(tc.text); got != tc.want {
			t.Fatalf("IsOrderCancelRequest(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestShouldCancelPersistedOrder(t *testing.T) {
	statusMsg := "maaf baru bales, saya ga jadi beli ya kok. apa sudah dibuatkan nomor pesanan untuk saya ?"
	if ShouldCancelPersistedOrder(statusMsg) {
		t.Fatal("soft regret + status question should not cancel persisted order")
	}
	if !ShouldCancelPersistedOrder("batalkan pesanan") {
		t.Fatal("explicit cancel should cancel persisted")
	}
	if !ShouldCancelPersistedOrder("batal") {
		t.Fatal("standalone batal should cancel persisted")
	}
}

func TestIsOrderCancelRequestLegacy(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"mau saya batalkan ya", true},
		{"batalkan pesanan", true},
		{"tidak jadi order", true},
		{"ga jadi deh", true},
		{"ga jadi mau dirubah menjadi 10 biji ya", false},
		{"loh ubah jadi 10 paket", false},
		{"pesanan yang atas nama saya ada kah ?", false},
		{"harga berapa", false},
	}
	for _, tc := range cases {
		if got := IsOrderCancelRequest(tc.text); got != tc.want {
			t.Fatalf("IsOrderCancelRequest(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestIsOrderStatusInquiry(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"pesanan yang atas nama saya ada kah ?", true},
		{"pembeli atas nama saya ada ?", true},
		{"saya masih punya pesanan aktif nggak ?", true},
		{"pembeli dengan nama Lavana Snack ada ?", false},
		{"status pesanan saya", true},
		{"order mana yang kamu batalkan ?", true},
		{"mau saya batalkan ya", false},
		{"halo kak", false},
		{"mau order boxer mono spot 10 paket bisa ?", false},
		{"loh saya mau order barang woi", false},
		{"bisa order boxer mono spot 5 paket?", false},
		{"pesanan ini sudah saya bayar belum WB-59505DFE?", true},
		{"yang sudah bayar yang mana?", true},
	}
	for _, tc := range cases {
		if got := IsOrderStatusInquiry(tc.text); got != tc.want {
			t.Fatalf("IsOrderStatusInquiry(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestIsCancelClarificationQuestion(t *testing.T) {
	for _, msg := range []string{
		"order mana yang kamu batalkan ?",
		"pesanan mana yang dibatalkan?",
		"kok order saya dibatalkan?",
		"kenapa dibatalkan?",
	} {
		if !IsCancelClarificationQuestion(msg) {
			t.Fatalf("IsCancelClarificationQuestion(%q) want true", msg)
		}
		if IsDraftOrderCancelRequest(msg) {
			t.Fatalf("IsDraftOrderCancelRequest(%q) want false for clarification", msg)
		}
	}
}

func TestIsExplicitNewOrderStart(t *testing.T) {
	for _, msg := range []string{
		"mau buat pesanan baru bisa ?",
		"loh, saya mau buat pesanan baru oi",
		"saya mau buat pesanan baru",
	} {
		if !IsExplicitNewOrderStart(msg) {
			t.Fatalf("IsExplicitNewOrderStart(%q) want true", msg)
		}
		if !IsNewPurchaseIntentQuestion(msg) {
			t.Fatalf("IsNewPurchaseIntentQuestion(%q) want true for new order start", msg)
		}
		if IsOrderStatusInquiry(msg) {
			t.Fatalf("IsOrderStatusInquiry(%q) want false for new order start", msg)
		}
	}
}

func TestIsNewPurchaseIntentQuestion(t *testing.T) {
	for _, msg := range []string{
		"mau order boxer mono spot 10 paket bisa ?",
		"loh saya mau order barang woi",
		"loh, saya mau buat pesanan baru oi",
		"bisa order de wasa 3 paket?",
		"mau pesan abon 2 biji boleh?",
	} {
		if !IsNewPurchaseIntentQuestion(msg) {
			t.Fatalf("IsNewPurchaseIntentQuestion(%q) want true", msg)
		}
		if IsOrderStatusInquiry(msg) {
			t.Fatalf("IsOrderStatusInquiry(%q) want false for new purchase", msg)
		}
	}
}

func TestParseOrderRefFromMessage(t *testing.T) {
	if got := parseOrderRefFromMessage("batalkan WB-EAA94534"); got != "WB-EAA94534" {
		t.Fatalf("parseOrderRefFromMessage = %q", got)
	}
	if parseOrderRefFromMessage("halo kak") != "" {
		t.Fatal("no ref expected")
	}
}

func TestFormatPersistedOrderSummary(t *testing.T) {
	o := &persistedOrder{
		ID:     "eb76635c-8439-42f1-9a45-dfa31bc0bbf4",
		Status: "draft",
		ItemsJSON: []byte(`[{"name":"Celana Dalam Boxer","qty":5,"unitPrice":21500}]`),
		ShippingJSON: []byte(`{"name":"Antoni","city":"Magelang"}`),
		Total: 107500,
	}
	summary := formatPersistedOrderSummary(o)
	if !strings.Contains(summary, "WB-EB76635C") {
		t.Fatalf("summary missing order ref: %s", summary)
	}
	if !strings.Contains(summary, "Celana Dalam Boxer") {
		t.Fatalf("summary missing product: %s", summary)
	}
}
