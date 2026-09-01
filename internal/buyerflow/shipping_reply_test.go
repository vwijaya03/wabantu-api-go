package buyerflow

import (
	"strings"
	"testing"
)

func TestIsShippingFAQQuestion(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"berapa lama pengiriman?", true},
		{"bisa kirim ke luar kota?", true},
		{"berapa ongkir ke surabaya?", true},
		{"harga jeans berapa?", false},
		{"toko jual apa aja?", false},
	}
	for _, tc := range cases {
		got := IsShippingFAQQuestion(tc.text)
		if got != tc.want {
			t.Fatalf("%q: got %v want %v", tc.text, got, tc.want)
		}
	}
}

func TestBuildShippingQuoteReply_template(t *testing.T) {
	area := "Jabodetabek & seluruh Indonesia"
	profile := &BusinessProfile{
		BusinessName: "Omah",
		DeliveryArea: &area,
	}
	reply := buildShippingQuoteReply("berapa ongkir ke surabaya?", profile, nil, false)
	if !strings.Contains(reply, "alamat lengkap") {
		t.Fatalf("expected address prompt: %s", reply)
	}
	if !strings.Contains(reply, "Jabodetabek") {
		t.Fatalf("expected delivery area: %s", reply)
	}
}

func TestTryShippingFAQReply_kbFirst(t *testing.T) {
	kb := []KBEntry{{
		Question: "berapa lama pengiriman",
		Answer:   "2-3 hari kerja via JNE.",
		IsActive: true,
	}}
	reply, ok := TryShippingFAQReply("berapa lama pengiriman?", nil, kb, false)
	if !ok || !strings.Contains(reply, "2-3 hari") {
		t.Fatalf("expected KB answer, got %q ok=%v", reply, ok)
	}
}

func TestTryShippingFAQReply_kbETA(t *testing.T) {
	cat := "Pengiriman"
	kb := []KBEntry{{
		Question: "berapa lama pengiriman",
		Answer:   "Estimasi pengiriman 2-3 hari kerja via JNE.",
		Category: &cat,
		IsActive: true,
	}}
	reply, ok := TryShippingFAQReply("berapa lama pengiriman?", nil, kb, false)
	if !ok || !strings.Contains(reply, "2-3 hari") {
		t.Fatalf("expected ETA KB, got %q ok=%v", reply, ok)
	}
}

func TestCatalogSkipsShippingFAQ(t *testing.T) {
	profile := omahProfile()
	_, ok := replyFromBusinessCatalog("berapa ongkir ke surabaya?", profile, omahCatalog(), nil, nil)
	if ok {
		t.Fatal("shipping quote must not be handled by catalog_db")
	}
}
