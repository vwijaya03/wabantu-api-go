package buyerflow

import (
	"strings"
	"testing"
)

func steakKBWithAccounts() []KBEntry {
	kb := omahSteakKB()
	pay := "pembayaran"
	return append(kb, KBEntry{ID: "kb-pay-accounts", Question: "Apa saja metode pembayaran yang tersedia ?", Answer: "Saat ini kami menerima pembayaran melalui Transfer Bank dan QRIS.\n\nBank Transfer:\nBCA - 000111222 - Omah Apparel\nMandiri - 333111222 - Omah Apparel", Category: &pay, IsActive: true})
}

func TestPaymentFAQNeverReturnsEmptyOK(t *testing.T) {
	pop := PushSavedBuyerLang(LangEN)
	defer pop()
	for _, q := range []string{"Where should i send payment?", "How do I pay?", "Can I pay with QRIS?", "how do i pay for my order"} {
		ans, ok := TryPaymentFAQAnswer(q, steakKBWithAccounts())
		if ok && strings.TrimSpace(ans) == "" {
			t.Errorf("TryPaymentFAQAnswer(%q) = (\"\", true): empty answers must fall through", q)
		}
	}
}

func TestPaymentFAQPrefersAccountEntry(t *testing.T) {
	pop := PushSavedBuyerLang(LangID)
	defer pop()
	ans, ok := TryPaymentFAQAnswer("Mau transfer ke rekening mana kak?", steakKBWithAccounts())
	if !ok || !strings.Contains(ans, "000111222") {
		t.Fatalf("got (%q,%v), want the entry with account numbers", ans, ok)
	}
}

func TestShippingFAQRecognisesDeliveredTodayQuestions(t *testing.T) {
	for _, q := range []string{"delivered today or tomorrow woi", "Is it going to be delivered today or tomorrow?", "can it arrive today?", "sampai hari ini atau besok?"} {
		if !IsShippingFAQQuestion(q) {
			t.Errorf("IsShippingFAQQuestion(%q) = false, want true", q)
		}
	}
}

func TestShippingKBNeverEmptyForEnglish(t *testing.T) {
	pop := PushSavedBuyerLang(LangEN)
	defer pop()
	reply, ok := TryShippingFAQReply("can you do same day delivery?", omahSteakProfile(), omahSteakKB(), false)
	if !ok || strings.TrimSpace(reply) == "" {
		t.Fatalf("got (%q,%v)", reply, ok)
	}
	if !strings.Contains(reply, "08:00") {
		t.Errorf("same-day question must mention the 08:00–12:00 dispatch rule, got %q", reply)
	}
}

func TestCheckoutShippingKBSameDayRuleComesBeforeETA(t *testing.T) {
	pop := PushSavedBuyerLang(LangEN)
	defer pop()
	for _, q := range []string{"can you do same day delivery?", "Is it going to be delivered today or tomorrow?"} {
		reply, ok := TryCheckoutActiveShippingFAQReply(q, "ask_recipient", omahSteakProfile(), omahSteakKB(), false)
		if !ok {
			t.Fatalf("%q: no reply", q)
		}
		dispatch, eta := strings.Index(reply, "08:00"), strings.Index(reply, "3-4 days")
		if dispatch < 0 || eta < 0 || dispatch > eta {
			t.Errorf("%q: want the 08:00 dispatch rule first, then the 3-4 days ETA, got %q", q, reply)
		}
	}
}

func TestCheckoutShippingKBSameDayIndonesian(t *testing.T) {
	pop := PushSavedBuyerLang(LangID)
	defer pop()
	reply, ok := TryCheckoutActiveShippingFAQReply("bisa kirim hari ini kak?", "ask_recipient", omahSteakProfile(), omahSteakKB(), false)
	if !ok {
		t.Fatal("no reply")
	}
	dispatch, eta := strings.Index(reply, "08:00"), strings.Index(reply, "3-4 hari")
	if dispatch < 0 || eta < 0 || dispatch > eta {
		t.Errorf("want the 08:00 dispatch rule first, then the 3-4 hari ETA, got %q", reply)
	}
}

func TestShippingKBFAQUnlocalizableAnswerIsNotAnAnswer(t *testing.T) {
	pop := PushSavedBuyerLang(LangEN)
	defer pop()
	ship := "pengiriman"
	// Indonesian answer with no English localizer: an English buyer must not get "" back as a hit.
	kb := []KBEntry{{ID: "kb-island", Question: "Apakah pengiriman ke luar pulau dikenakan biaya tambahan?", Answer: "Pengiriman ke luar pulau dikenakan biaya tambahan sesuai kurir kami.", Category: &ship, IsActive: true}}
	if ans, ok := tryShippingKBFAQ("how long does shipping to other islands take", kb); ok {
		t.Errorf("tryShippingKBFAQ = (%q,%v), want no KB answer", ans, ok)
	}
	for _, step := range []string{"ask_recipient", "ask_address_full", ""} {
		reply, ok := TryCheckoutActiveShippingFAQReply("how long does shipping to other islands take", step, omahSteakProfile(), kb, false)
		if !ok || strings.TrimSpace(reply) == "" {
			t.Errorf("step %q: got (%q,%v), want a non-empty fallback answer", step, reply, ok)
		}
		if !strings.Contains(reply, "courier") {
			t.Errorf("step %q: want the generic English delivery-timing answer, got %q", step, reply)
		}
	}
}
