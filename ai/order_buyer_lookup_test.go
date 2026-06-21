package ai

import (
	"strings"
	"testing"
)

func TestIsThirdPartyBuyerLookup(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"pembeli dengan nama Lavana Snack ada ?", true},
		{"customer dengan nama Budi ada?", true},
		{"data pembeli Anton", true},
		{"pembeli atas nama saya ada ?", false},
		{"pembeli atas nama ini ada? Nama: supriyanto", false},
		{"saya masih punya pesanan aktif nggak ?", false},
		{"status pesanan WB-58D662BC", false},
		{"mau order boxer 10 paket", false},
	}
	for _, tc := range cases {
		if got := IsThirdPartyBuyerLookup(tc.text); got != tc.want {
			t.Fatalf("IsThirdPartyBuyerLookup(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestIsSelfBuyerOrderLookup(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"pembeli atas nama saya ada ?", true},
		{"pembeli atas nama ini ada? Nama: supriyanto", true},
		{"pembeli saya ada?", true},
		{"pembeli dengan nama Lavana Snack ada ?", false},
		{"mau order boxer 10 paket", false},
		{"batalkan pesanan", false},
	}
	for _, tc := range cases {
		if got := IsSelfBuyerOrderLookup(tc.text); got != tc.want {
			t.Fatalf("IsSelfBuyerOrderLookup(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestParseRecipientHintFromMessage(t *testing.T) {
	name, phone := parseRecipientHintFromMessage("pembeli atas nama ini ada? Nama: supriyanto")
	if name != "supriyanto" {
		t.Fatalf("name = %q, want supriyanto", name)
	}
	if phone != "" {
		t.Fatalf("phone = %q, want empty", phone)
	}
	name, phone = parseRecipientHintFromMessage("Nama: Budi\nHP: 08123456789")
	if name != "Budi" || phone != "+628123456789" {
		t.Fatalf("got name=%q phone=%q", name, phone)
	}
}

func TestParseOrderRefFromHistory(t *testing.T) {
	history := []dbMessage{
		{Direction: "in", Body: "halo"},
		{Direction: "out", Body: "Pesanan WB-58D662BC sudah kami terima."},
		{Direction: "in", Body: "pesanan tadi gimana?"},
	}
	if got := parseOrderRefFromHistory(history); got != "WB-58D662BC" {
		t.Fatalf("parseOrderRefFromHistory = %q", got)
	}
	if parseOrderRefFromHistory(nil) != "" {
		t.Fatal("empty history should return empty ref")
	}
}

func TestResolveOrderRefFromUserOrHistory(t *testing.T) {
	history := []dbMessage{
		{Direction: "out", Body: "Nomor pesanan: WB-AABBCCDD"},
	}
	if got := resolveOrderRefFromUserOrHistory("WB-11223344", history); got != "WB-11223344" {
		t.Fatalf("explicit ref = %q", got)
	}
	if got := resolveOrderRefFromUserOrHistory("pesanan tadi gimana?", history); got != "WB-AABBCCDD" {
		t.Fatalf("history ref = %q", got)
	}
	if got := resolveOrderRefFromUserOrHistory("pembeli atas nama saya ada?", history); got != "" {
		t.Fatalf("self buyer without context should not use history, got %q", got)
	}
}

func TestTryFAQSkipsOrderLookupIntents(t *testing.T) {
	kb := []dbKBEntry{
		{Question: "pembeli", Answer: "Maaf kami tidak bisa memberikan data pembeli lain."},
	}
	for _, msg := range []string{
		"pembeli dengan nama Lavana Snack ada ?",
		"pembeli atas nama saya ada ?",
		"pembeli atas nama ini ada? Nama: supriyanto",
		"saya masih punya pesanan aktif nggak ?",
	} {
		if _, ok := tryFAQDirectAnswer(msg, kb); ok {
			t.Fatalf("tryFAQDirectAnswer should skip order lookup: %q", msg)
		}
	}
}

func TestThirdPartyBuyerLookupDeniedReply(t *testing.T) {
	reply := thirdPartyBuyerLookupDeniedReply()
	if !strings.Contains(reply, "nomor WhatsApp ini") {
		t.Fatalf("deny reply should mention scoped access: %q", reply)
	}
}

func TestOrderRecipientHintNotFoundReply(t *testing.T) {
	reply := orderRecipientHintNotFoundReply()
	if !strings.Contains(reply, "Tidak ada pesanan") {
		t.Fatalf("hint not found reply: %q", reply)
	}
}
