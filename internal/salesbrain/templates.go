package salesbrain

import (
	"fmt"
	"sort"
	"strings"
)

// TemplateKey names a fixed system/transaction message that exists in both languages.
type TemplateKey string

const (
	TplLanguageChoice   TemplateKey = "language_choice"    // %[1]s shop name (bilingual by design)
	TplSwitchConfirm    TemplateKey = "switch_confirm"     // no args
	TplSystemBusy       TemplateKey = "system_busy"        // %[1]s shop name
	TplHandoffTakeover  TemplateKey = "handoff_takeover"   // %[1]s shop name, %[2]s opening-hours sentence (may be "")
	TplHandoffResumed   TemplateKey = "handoff_resumed"    // no args
	TplQuotaExhausted   TemplateKey = "quota_exhausted"    // %[1]s shop name
	TplResumeRecipient  TemplateKey = "resume_recipient"   // no args
	TplResumeAddress    TemplateKey = "resume_address"     // no args
	TplResumeConfirm    TemplateKey = "resume_confirm"     // no args
	TplOpenOrderOptions TemplateKey = "open_order_options" // %[1]s DescribeOrder text
	TplOpenOrderPick    TemplateKey = "open_order_pick"    // %[1]s numbered list
	TplOrderCancelled   TemplateKey = "order_cancelled"    // %[1]s order ref
	TplCancelPaidOrder  TemplateKey = "cancel_paid_order"  // %[1]s order ref, %[2]s shop name
	TplNewOrderParked   TemplateKey = "new_order_parked"   // %[1]s order ref
	TplCheckWithTeam    TemplateKey = "check_with_team"    // no args
	TplOrderRef         TemplateKey = "order_ref"          // %[1]s order ref, appended after the order was saved
	TplFieldProblems    TemplateKey = "field_problems"     // %[1]s invalid-data fragments joined by the caller
	TplShippingSummary  TemplateKey = "shipping_summary"   // %[1]s name, %[2]s phone, %[3]s address line
)

var templates = map[TemplateKey]map[Lang]string{
	TplLanguageChoice: {
		LangID: "Halo, selamat datang di %[1]s! Mau pakai Bahasa Indonesia atau English?\nHello, welcome to %[1]s! Would you like Bahasa Indonesia or English?",
		LangEN: "Halo, selamat datang di %[1]s! Mau pakai Bahasa Indonesia atau English?\nHello, welcome to %[1]s! Would you like Bahasa Indonesia or English?",
	},
	TplSwitchConfirm: {
		LangID: "Siap, aku lanjut pakai Bahasa Indonesia ya.",
		LangEN: "Sure, I'll reply in English from now on.",
	},
	TplSystemBusy: {
		LangID: "Maaf Kak, sistem kami sedang ada gangguan. Pesan Kakak sudah kami terima dan tim %[1]s akan membalas secepatnya.",
		LangEN: "Sorry, our system is having a problem right now. We've received your message and the %[1]s team will reply as soon as possible.",
	},
	TplHandoffTakeover: {
		LangID: "Baik Kak, aku sambungkan ke tim %[1]s sekarang supaya Kakak tidak perlu mengulang cerita. %[2]s",
		LangEN: "Sure, I'm passing this chat to the %[1]s team now so you won't need to repeat anything. %[2]s",
	},
	TplHandoffResumed: {
		LangID: "Maaf menunggu, Kak. Tim kami belum sempat membalas, jadi aku lanjut bantu dulu ya; pertanyaan Kakak tetap aku teruskan.",
		LangEN: "Sorry for the wait. The team hasn't replied yet, so I'll keep helping here; your question is still with them.",
	},
	TplQuotaExhausted: {
		LangID: "Maaf Kak, balasan otomatis sedang tidak tersedia. Tim %[1]s akan membalas langsung.",
		LangEN: "Sorry, automatic replies are unavailable right now. The %[1]s team will reply to you directly.",
	},
	TplResumeRecipient: {
		LangID: "Lanjut ya Kak: nama dan nomor HP penerimanya siapa?",
		LangEN: "Back to your order: what's the recipient's name and phone number?",
	},
	TplResumeAddress: {
		LangID: "Lanjut ya Kak: alamat pengirimannya di mana (jalan, kecamatan, kota, kode pos)?",
		LangEN: "Back to your order: what's the delivery address (street, district, city, postal code)?",
	},
	TplResumeConfirm: {
		LangID: "Kalau sudah benar, balas OK ya Kak biar aku proses.",
		LangEN: "If everything looks right, reply OK and I'll process it.",
	},
	TplOpenOrderOptions: {
		LangID: "Kakak masih punya pesanan yang belum selesai, %[1]s. Mau dilanjutkan, bikin pesanan baru, atau pesanan ini dibatalkan?",
		LangEN: "You still have an unfinished order, %[1]s. Would you like to continue it, start a new order, or cancel this one?",
	},
	TplOpenOrderPick: {
		LangID: "Kakak punya beberapa pesanan yang belum selesai:\n%[1]s\nYang mana yang mau dilanjutkan, Kak?",
		LangEN: "You have a few unfinished orders:\n%[1]s\nWhich one would you like to continue?",
	},
	TplOrderCancelled: {
		LangID: "Siap Kak, pesanan %[1]s sudah dibatalkan. Mau pesan yang baru?",
		LangEN: "Done, order %[1]s is cancelled. Would you like to start a new one?",
	},
	TplCancelPaidOrder: {
		LangID: "Pesanan %[1]s sudah dibayar, jadi pembatalannya aku teruskan ke tim %[2]s ya Kak.",
		LangEN: "Order %[1]s has already been paid, so I've passed the cancellation request to the %[2]s team.",
	},
	TplNewOrderParked: {
		LangID: "Siap Kak, pesanan %[1]s tetap tersimpan; bilang saja kalau mau dilanjutkan atau dibatalkan. Mau pesan apa?",
		LangEN: "Sure, order %[1]s stays saved; just tell me if you want to continue or cancel it. What would you like to order?",
	},
	TplCheckWithTeam: {
		LangID: "Sebentar ya Kak, aku cek dulu ke tim toko dan kabari di chat ini.",
		LangEN: "Let me double-check that with the shop team and get back to you here.",
	},
	TplOrderRef: {
		LangID: "Nomor pesanan: %[1]s",
		LangEN: "Order number: %[1]s",
	},
	TplFieldProblems: {
		LangID: "Maaf Kak, %[1]s.",
		LangEN: "Sorry, %[1]s.",
	},
	TplShippingSummary: {
		LangID: "Dikirim ke:\n%[1]s (%[2]s)\n%[3]s",
		LangEN: "Shipping to:\n%[1]s (%[2]s)\n%[3]s",
	},
}

// T renders a template in the given language (falls back to Indonesian only if a language
// entry is missing, which TestEveryTemplateHasBothLanguages forbids). Unknown keys → "".
func T(key TemplateKey, lang Lang, args ...any) string {
	byLang, ok := templates[key]
	if !ok {
		return ""
	}
	s := byLang[lang]
	if s == "" {
		s = byLang[LangID]
	}
	if len(args) == 0 {
		return s
	}
	return strings.TrimSpace(fmt.Sprintf(s, args...))
}

// TemplateKeys lists every template key in a stable order.
func TemplateKeys() []TemplateKey {
	keys := make([]TemplateKey, 0, len(templates))
	for k := range templates {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// OpeningHoursSentence tells the buyer when the team usually replies, without promising a
// time when the tenant has no opening hours configured.
func OpeningHoursSentence(hours string, lang Lang) string {
	hours = strings.TrimSpace(hours)
	if hours == "" {
		return ""
	}
	if lang == LangEN {
		return fmt.Sprintf("They usually reply during opening hours (%s).", hours)
	}
	return fmt.Sprintf("Biasanya dibalas di jam buka (%s).", hours)
}
