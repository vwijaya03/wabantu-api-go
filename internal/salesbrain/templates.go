package salesbrain

import (
	"fmt"
	"sort"
	"strings"
)

// TemplateKey names a fixed system/transaction message that exists in both languages.
type TemplateKey string

const (
	TplLanguageChoice      TemplateKey = "language_choice"      // %[1]s shop name (bilingual by design)
	TplSwitchConfirm       TemplateKey = "switch_confirm"       // no args
	TplSystemBusy          TemplateKey = "system_busy"          // %[1]s shop name
	TplHandoffTakeover     TemplateKey = "handoff_takeover"     // %[1]s shop name, %[2]s opening-hours sentence (may be "")
	TplHandoffResumed      TemplateKey = "handoff_resumed"      // no args
	TplQuotaExhausted      TemplateKey = "quota_exhausted"      // %[1]s shop name
	TplResumeRecipient     TemplateKey = "resume_recipient"     // no args
	TplResumeAddress       TemplateKey = "resume_address"       // no args
	TplResumeConfirm       TemplateKey = "resume_confirm"       // no args
	TplOpenOrderOptions    TemplateKey = "open_order_options"   // %[1]s DescribeOrder text
	TplOpenOrderPick       TemplateKey = "open_order_pick"      // %[1]s numbered list
	TplOrderCancelled      TemplateKey = "order_cancelled"      // %[1]s order ref
	TplCancelPaidOrder     TemplateKey = "cancel_paid_order"    // %[1]s order ref, %[2]s shop name
	TplNewOrderParked      TemplateKey = "new_order_parked"     // %[1]s order ref
	TplCheckWithTeam       TemplateKey = "check_with_team"      // no args
	TplOrderRef            TemplateKey = "order_ref"            // %[1]s order ref, appended after the order was saved
	TplFieldProblems       TemplateKey = "field_problems"       // %[1]s invalid-data fragments joined by the caller
	TplFieldSaved          TemplateKey = "field_saved"          // %[1]s what was stored this turn (name, phone, address)
	TplFieldMissing        TemplateKey = "field_missing"        // %[1]s what is still missing
	TplShippingSummary     TemplateKey = "shipping_summary"     // %[1]s name, %[2]s phone, %[3]s address line
	TplConfirmHold         TemplateKey = "confirm_hold"         // no args; lead-in when the confirm step is held (fail-closed)
	TplResumeVariant       TemplateKey = "resume_variant"       // no args
	TplAskVariant          TemplateKey = "ask_variant"          // %[1]s product name
	TplAskVariantOptions   TemplateKey = "ask_variant_options"  // %[1]s product name, %[2]s variants in stock (sizes and/or colours)
	TplAskVariantChoices   TemplateKey = "ask_variant_choices"  // %[1]s product name, %[2]s sizes in stock
	TplVariantNotFound     TemplateKey = "variant_not_found"    // %[1]s product name, %[2]s sizes in stock
	TplColorNotFound       TemplateKey = "color_not_found"      // %[1]s product name, %[2]s colours sold
	TplVariantSoldOut      TemplateKey = "variant_sold_out"     // %[1]s product name, %[2]s alternatives sentence (may be "")
	TplVariantAlternatives TemplateKey = "variant_alternatives" // %[1]s sizes in stock
	TplStockLimit          TemplateKey = "stock_limit"          // %[1]s product name, %[2]d units left
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
	TplFieldSaved: {
		LangID: "Sudah kucatat ya Kak: %[1]s.",
		LangEN: "Noted: %[1]s.",
	},
	TplFieldMissing: {
		LangID: "Yang masih kurang: %[1]s.",
		LangEN: "Still missing: %[1]s.",
	},
	TplShippingSummary: {
		LangID: "Dikirim ke:\n%[1]s (%[2]s)\n%[3]s",
		LangEN: "Shipping to:\n%[1]s (%[2]s)\n%[3]s",
	},
	TplConfirmHold: {
		LangID: "Maaf Kak, balasan Kakak barusan belum bisa aku proses dengan pasti, jadi pesanan belum aku simpan atau ubah. Ini ringkasannya:",
		LangEN: "Sorry, I couldn't process your last message reliably, so your order has not been saved or changed. Here is the summary:",
	},
	TplResumeVariant: {
		LangID: "Lanjut ya Kak: ukuran atau warna yang mana untuk pesanannya?",
		LangEN: "Back to your order: which size or color would you like?",
	},
	TplAskVariant: {
		LangID: "Untuk %[1]s, Kakak mau ukuran atau warna apa?",
		LangEN: "Which size or color would you like for %[1]s?",
	},
	TplAskVariantOptions: {
		LangID: "Untuk %[1]s, pilihan yang tersedia: %[2]s. Kakak mau yang mana?",
		LangEN: "For %[1]s the available options are: %[2]s. Which one would you like?",
	},
	TplAskVariantChoices: {
		LangID: "%[1]s ada dalam ukuran: %[2]s. Kakak mau yang mana?",
		LangEN: "%[1]s comes in these sizes: %[2]s. Which one would you like?",
	},
	TplVariantNotFound: {
		LangID: "Maaf Kak, ukuran itu tidak ada untuk %[1]s. Yang tersedia: %[2]s. Kakak mau yang mana?",
		LangEN: "Sorry, that size isn't available for %[1]s. Available: %[2]s. Which one would you like?",
	},
	TplColorNotFound: {
		LangID: "Maaf Kak, warna itu tidak kami jual untuk %[1]s. Warna yang tersedia: %[2]s. Kakak mau yang mana?",
		LangEN: "Sorry, we don't sell that colour for %[1]s. Available colours: %[2]s. Which would you like?",
	},
	TplVariantSoldOut: {
		LangID: "Maaf Kak, %[1]s sedang habis. %[2]s",
		LangEN: "Sorry, %[1]s is out of stock. %[2]s",
	},
	TplVariantAlternatives: {
		LangID: "Yang masih tersedia: %[1]s. Mau pilih yang mana, Kak?",
		LangEN: "Still available: %[1]s. Which would you like?",
	},
	TplStockLimit: {
		LangID: "Maaf Kak, stok %[1]s tinggal %[2]d. Mau pesan sejumlah itu?",
		LangEN: "Sorry, only %[2]d of %[1]s left. Would you like that many?",
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
