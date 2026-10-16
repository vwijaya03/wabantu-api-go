package ai

import (
	"testing"
)

func omahSteakKBEntries() []dbKBEntry {
	ship := "pengiriman"
	pay := "pembayaran"
	return []dbKBEntry{
		{ID: "kb-eta", Question: "Berapa lama estimasi pengiriman pesanan saya?", Answer: "Estimasi pengiriman pesanan Anda adalah 3-4 hari.", Category: &ship},
		{ID: "kb-courier", Question: "Metode pengiriman apa yang digunakan?", Answer: "Kami melayani pengiriman ke seluruh Indonesia menggunakan jasa kurir J&T dan JNE.", Category: &ship},
		{ID: "kb-same-day-2", Question: "Kapan pesanan saya akan dikirim?", Answer: "Pesanan dan pembayaran yang kami terima antara pukul 08:00 hingga 12:00 WIB akan dikirim di hari yang sama. Pesanan setelah pukul 12:00 WIB akan dikirim pada hari kerja berikutnya.", Category: &ship},
		{ID: "kb-pay-method", Question: "Apa saja metode pembayaran yang tersedia?", Answer: "Saat ini kami menerima pembayaran melalui Transfer Bank dan QRIS.", Category: &pay},
	}
}

func TestScoreKBEntriesEnglishSameDayShippingQuestion(t *testing.T) {
	kb := omahSteakKBEntries()
	query := "Then can i order it today and send it also today?"
	scored := scoreKBEntries(query, kb)
	if len(scored) == 0 {
		t.Fatal("expected lexical KB hits for English same-day question")
	}
	top := scored[0].entry.ID
	if top != "kb-same-day-2" && top != "kb-same-day" {
		found := false
		for i := 0; i < len(scored) && i < 3; i++ {
			if scored[i].entry.ID == "kb-same-day-2" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected same-day KB in top 3, got first=%s scored=%+v", top, scored)
		}
	}
}

func TestScoreKBEntriesEnglishDeliveryFeeQuestion(t *testing.T) {
	kb := omahSteakKBEntries()
	scored := scoreKBEntries("how much is the delivery fee to jakarta?", kb)
	if len(scored) == 0 {
		t.Fatal("expected shipping KB hits for delivery fee question")
	}
}
