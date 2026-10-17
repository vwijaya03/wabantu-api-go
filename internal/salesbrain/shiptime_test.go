package salesbrain

import (
	"strings"
	"testing"
	"time"
)

var wibZone = time.FixedZone("WIB", 7*3600)

func omahShipKB() []KBFact {
	return []KBFact{
		{Question: "Kapan pesanan saya akan dikirim?", Answer: "Pesanan dan pembayaran yang kami terima antara pukul 08:00 hingga 12:00 WIB akan dikirim di hari yang sama. Pesanan setelah pukul 12:00 WIB akan dikirim pada hari kerja berikutnya."},
	}
}

func TestShipIfPaidNow(t *testing.T) {
	day := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, wibZone) } // 19 Oct 2026 = Monday
	for _, tc := range []struct {
		name string
		now  time.Time
		want string
	}{
		{"incident 00:18 on a working day", day(19, 0, 18), "hari ini bila dibayar antara 08:00–12:00"},
		{"inside the window", day(19, 9, 30), "hari ini"},
		{"after the cut-off", day(19, 12, 1), "hari kerja berikutnya"},
		{"exactly at the cut-off", day(19, 12, 0), "hari kerja berikutnya"},
		{"night", day(19, 23, 0), "hari kerja berikutnya"},
		{"Sunday 00:18 (the incident date)", day(18, 0, 18), "hari kerja berikutnya"},
		{"Saturday morning", day(17, 9, 0), "hari kerja berikutnya"},
	} {
		if got := ShipIfPaidNow(tc.now, omahShipKB()); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestShipIfPaidNowOmittedWithoutParseableCutoff(t *testing.T) {
	kb := []KBFact{{Question: "Estimasi kirim?", Answer: "Estimasi pengiriman 3-4 hari."}, {Question: "Metode bayar?", Answer: "Transfer, jam 08:00 - 12:00 kami online."}}
	if got := ShipIfPaidNow(time.Date(2026, 10, 19, 9, 0, 0, 0, wibZone), kb); got != "" {
		t.Fatalf("no window in the FAQ: want no line, got %q", got)
	}
	if got := ShipIfPaidNow(time.Date(2026, 10, 19, 9, 0, 0, 0, wibZone), nil); got != "" {
		t.Fatalf("empty FAQ: got %q", got)
	}
	cut := []KBFact{{Question: "Cut off kirim", Answer: "Cut off pengiriman pukul 14.00 WIB."}}
	if got := ShipIfPaidNow(time.Date(2026, 10, 19, 13, 0, 0, 0, wibZone), cut); got != "hari ini" {
		t.Fatalf("cut-off 14.00: got %q", got)
	}
}

func TestStateLineAndPromptRuleForShipping(t *testing.T) {
	_, user := BuildPrompt(PromptInput{State: TurnState{Lang: LangEN, ShipIfPaidNow: "hari kerja berikutnya"}})
	if !strings.Contains(user, "pengiriman_jika_bayar_sekarang: hari kerja berikutnya") {
		t.Fatalf("state line missing:\n%s", user)
	}
	_, user = BuildPrompt(PromptInput{State: TurnState{Lang: LangEN}})
	if strings.Contains(user, "pengiriman_jika_bayar_sekarang") {
		t.Fatal("no line when unknown")
	}
	system, _ := BuildPrompt(PromptInput{})
	if !strings.Contains(system, "JANGAN menghitung atau menyimpulkan jam sendiri") {
		t.Fatal("persona must tell the model to use the line")
	}
}
