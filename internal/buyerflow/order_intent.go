package buyerflow

import (
	"regexp"
	"strings"
)

var orderCancelPhrases = []string{
	"tidak jadi", "gak jadi", "ga jadi", "nggak jadi", "tidak order", "gak order",
	"batal order", "batal pesan", "cancel order", "batalkan order", "dibatalkan",
	"mau batalkan", "mau saya batalkan", "saya batalkan", "batalkan ya", "batalin",
	"batalkan pesanan", "batal pesanan", "cancel pesanan",
}

var softCancelRegretPhrases = []string{
	"tidak jadi", "gak jadi", "ga jadi", "nggak jadi", "tidak order", "gak order",
}

var explicitPersistedCancelPhrases = []string{
	"batalkan pesanan", "batal pesanan", "cancel order", "batalkan order",
	"cancel pesanan", "batal order", "batal pesan", "batalkan ya", "batalin",
	"mau batalkan", "mau saya batalkan", "saya batalkan", "dibatalkan",
}

// batal | batalkan | cancel (standalone atau dalam frasa pendek).
var orderCancelWordRe = regexp.MustCompile(`(?i)\b(batal(?:kan)?|cancel)\b`)

// Nomor pesanan singkat dari chat pembeli (WB-A1B2C3D4).
var orderRefInMessageRe = regexp.MustCompile(`(?i)\bWB-([A-F0-9]{6,8})\b`)

// Inline Nama:/HP: pada satu baris chat (mis. "... ada? Nama: supriyanto").
var recipientNameInlineRe = regexp.MustCompile(`(?i)\bnama\s*:\s*([^?\n,;]+)`)
var recipientPhoneInlineRe = regexp.MustCompile(`(?i)\b(?:hp|no\.?\s*hp|telp)\s*:\s*(\+?[\d\s-]{8,})`)

func isRevisionNotCancel(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	return orderRevisionSignals(text, userText)
}

// IsCancelClarificationQuestion — "order mana yang dibatalkan?" (status, bukan perintah batal).
func IsCancelClarificationQuestion(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	hasCancel := orderCancelWordRe.MatchString(text) ||
		strings.Contains(text, "dibatalkan") || strings.Contains(text, "dibatalin") ||
		strings.Contains(text, "di cancel") || strings.Contains(text, "di-cancel")
	if !hasCancel {
		return false
	}
	clarify := []string{
		"mana", "which", "kok", "kenapa", "mengapa",
		"yang kamu", "yang lu", "yang lo", "yang elu",
		"order apa", "pesanan apa", "order mana", "pesanan mana",
		"yang dibatalkan", "yang dibatalin", "barusan", "tadi",
	}
	for _, s := range clarify {
		if strings.Contains(text, s) {
			return true
		}
	}
	return strings.Contains(text, "?") &&
		(strings.Contains(text, "order") || strings.Contains(text, "pesanan"))
}

// ShouldKeepCartOnExplicitNewOrder — "pesanan baru" setelah pick leftover: keranjang
// Redis belum ter-pin, jadi jangan di-clear; persist akan INSERT draft baru.
func ShouldKeepCartOnExplicitNewOrder(st *OrderState, userText string) bool {
	if st == nil || !IsExplicitNewOrderStart(userText) {
		return false
	}
	if strings.TrimSpace(st.PersistedOrderID) != "" {
		return false
	}
	return st.CartReadyForDraft()
}

// IsExplicitNewOrderStart — "mau buat pesanan baru", bukan cek status pesanan lama.
func IsExplicitNewOrderStart(userText string) bool {
	text := normalizeBuyerTextForRules(userText)
	if text == "" {
		return false
	}
	for _, p := range []string{
		"pesanan baru", "buat pesanan", "order baru", "pesan baru", "buat order",
	} {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// IsNewPurchaseIntentQuestion — "mau order X bisa?" bukan tanya status pesanan lama.
func IsNewPurchaseIntentQuestion(userText string) bool {
	if IsPaymentStatusInquiry(userText) {
		return false
	}
	if IsCancelClarificationQuestion(userText) {
		return false
	}
	text := normalizeBuyerTextForRules(userText)
	if text == "" {
		return false
	}
	for _, p := range orderStatusInquiryPhrases {
		if strings.Contains(text, p) {
			return false
		}
	}
	if IsConsultingPurchaseQuestion(userText, nil) {
		return true
	}
	wants := strings.Contains(text, "mau") || hasOrderIntentText(userText)
	if !wants {
		return false
	}
	if IsExplicitNewOrderStart(userText) ||
		strings.Contains(text, "order barang") || strings.Contains(text, "pesan barang") ||
		strings.Contains(text, "beli barang") {
		return true
	}
	if mentionsOrderQty(text) {
		return true
	}
	for _, kw := range apparelProductKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return hasExplicitCartReadyPhrase(text)
}

// IsDraftOrderCancelRequest — batalkan draft Redis (termasuk "batal" / "cancel" saja).
func IsDraftOrderCancelRequest(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || isRevisionNotCancel(userText) || IsCancelClarificationQuestion(userText) ||
		IsCartLineCorrectionIntent(userText) || IsNegatedFullOrderCancel(userText) {
		return false
	}
	if orderCancelWordRe.MatchString(text) {
		return true
	}
	for _, p := range explicitPersistedCancelPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// IsSoftCancelRegret — "ga jadi" tanpa kata batal eksplisit.
func IsSoftCancelRegret(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || isRevisionNotCancel(userText) {
		return false
	}
	for _, p := range softCancelRegretPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// IsExplicitPersistedOrderCancel — batalkan order DB yang sudah tersimpan.
func IsExplicitPersistedOrderCancel(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || isRevisionNotCancel(userText) || IsCancelClarificationQuestion(userText) ||
		IsCartLineCorrectionIntent(userText) || IsNegatedFullOrderCancel(userText) {
		return false
	}
	for _, p := range explicitPersistedCancelPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	// "batal", "cancel", "batalkan" saja → eksplisit.
	fields := strings.Fields(strings.TrimRight(text, "?!. "))
	if len(fields) <= 2 && orderCancelWordRe.MatchString(text) {
		return true
	}
	return false
}

// ShouldCancelPersistedOrder — jangan batalkan order lama jika user cuma menyesal + tanya status.
func ShouldCancelPersistedOrder(userText string) bool {
	if IsExplicitPersistedOrderCancel(userText) {
		return true
	}
	if IsSoftCancelRegret(userText) && !IsOrderStatusInquiry(userText) &&
		!strings.Contains(strings.ToLower(userText), "?") {
		return true
	}
	return false
}

// IsOrderCancelRequest — sinyal batal (draft atau persisted routing di autoreply).
func IsOrderCancelRequest(userText string) bool {
	return IsDraftOrderCancelRequest(userText) || IsSoftCancelRegret(userText)
}

// IsOrderFlowCancelled kept for callers; same as cancel request.
func IsOrderFlowCancelled(userText string) bool {
	return IsOrderCancelRequest(userText)
}

var orderStatusInquiryPhrases = []string{
	"pesanan saya", "pesanan ku", "order saya", "ada pesanan", "punya pesanan",
	"punya order", "ada order",
	"status pesanan", "nomor pesanan", "no pesanan", "cek pesanan", "lihat pesanan",
	"detail pesanan", "lihat detail",
	"pesanan yang", "pesanan atas nama", "orderan saya", "pesanan mana",
	"apakah saya punya", "saya punya pesanan", "masih punya pesanan",
	"pembeli atas nama saya", "pembeli saya", "pembeli atas nama ini",
}

// IsOrderRefStatusLookup — buyer sends an order ref (with or without short status/detail phrasing).
func IsOrderRefStatusLookup(userText string) bool {
	if IsCartLineCorrectionIntent(userText) || IsNegatedFullOrderCancel(userText) {
		return false
	}
	if IsCheckoutMergeIntent(userText) {
		return false
	}
	ref := parseOrderRefFromMessage(userText)
	if ref == "" {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	remainder := strings.ReplaceAll(text, strings.ToLower(ref), "")
	remainder = strings.TrimSpace(strings.NewReplacer("?", "", "!", "", ".", "", ",", "").Replace(remainder))
	if remainder == "" || len(remainder) <= 4 {
		return true
	}
	for _, p := range []string{
		"status", "detail", "cek", "lihat", "gimana", "bagaimana", "progress", "update",
	} {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

var selfBuyerLookupPhrases = []string{
	"pembeli atas nama saya", "pembeli saya ada", "pembeli atas nama ini",
	"pembeli saya", "atas nama saya ada", "pembeli ini ada",
}

var thirdPartyBuyerLookupPhrases = []string{
	"pembeli dengan nama", "customer dengan nama", "data pembeli",
	"pelanggan dengan nama", "pembeli atas nama ", "customer atas nama",
}

var orderHistoryContextPhrases = []string{
	"pesanan tadi", "order tadi", "yang barusan", "yang tadi", "nomor tadi",
	"pesanan yang barusan", "yang kamu kirim", "pesanan yang dikirim",
}

// WantsActiveOrderOnly — tanya pesanan aktif/pending, bukan riwayat dibatalkan.
func WantsActiveOrderOnly(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	for _, s := range []string{
		"pending", "aktif", "masih punya", "masih ada", "belum selesai",
		"belum dibatalkan", "masih order", "masih pesanan",
	} {
		if strings.Contains(text, s) {
			return true
		}
	}
	if (strings.Contains(text, "punya pesanan") || strings.Contains(text, "ada pesanan") ||
		strings.Contains(text, "punya order") || strings.Contains(text, "ada order")) &&
		(strings.Contains(text, "nggak") || strings.Contains(text, " gak") ||
			strings.Contains(text, " ga") || strings.Contains(text, "tidak") ||
			strings.Contains(text, "?")) {
		return true
	}
	return false
}

// IsPaymentRejectionInquiry — buyer asks why proof was rejected.
func IsPaymentRejectionInquiry(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	for _, p := range []string{
		"kenapa ditolak", "kok ditolak", "mengapa ditolak",
		"kenapa bukti", "kok bukti", "bukti ditolak",
	} {
		if strings.Contains(text, p) {
			return true
		}
	}
	return strings.Contains(text, "ditolak") &&
		(strings.Contains(text, "bukti") || strings.Contains(text, "transfer"))
}

// IsPaymentStatusInquiry — buyer asks whether payment/proof was received or which order is paid.
func IsPaymentStatusInquiry(userText string) bool {
	if IsPaymentRejectionInquiry(userText) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	for _, p := range []string{
		"sudah bayar", "sudah dibayar", "sudah transfer", "sudah tf",
		"bukti bayar", "bukti transfer", "bukti pembayaran",
		"status bayar", "status pembayaran", "pembayaran sudah",
		"yang sudah bayar", "sudah lunas", "sudah kirim bukti",
	} {
		if strings.Contains(text, p) {
			return true
		}
	}
	if parseOrderRefFromMessage(userText) != "" &&
		(strings.Contains(text, "bayar") || strings.Contains(text, "transfer") ||
			strings.Contains(text, "bukti") || strings.Contains(text, "tf")) {
		return true
	}
	return false
}

// IsOrderStatusInquiry — customer asks about their existing order.
func IsOrderStatusInquiry(userText string) bool {
	if IsCartLineCorrectionIntent(userText) || IsNegatedFullOrderCancel(userText) {
		return false
	}
	if IsCheckoutMergeIntent(userText) {
		return false
	}
	if IsCartRecapOrComplaint(userText, nil) {
		return false
	}
	if IsAddMoreItemsPolicyQuestion(userText) {
		return false
	}
	if IsPaymentStatusInquiry(userText) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if IsMinimumOrderQuestion(userText) {
		return false
	}
	for _, p := range orderStatusInquiryPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	if IsCancelClarificationQuestion(userText) {
		return true
	}
	if IsNewPurchaseIntentQuestion(userText) {
		return false
	}
	if IsOrderCancelRequest(userText) {
		return false
	}
	return (strings.Contains(text, "pesanan") || strings.Contains(text, "order")) &&
		(strings.Contains(text, "?") || strings.Contains(text, "ada") ||
			strings.Contains(text, "berapa") || strings.Contains(text, "cek") ||
			strings.Contains(text, "status") || strings.Contains(text, "nomor"))
}

// parseRecipientHintFromMessage extracts Nama:/HP: blocks from buyer chat text.
func parseRecipientHintFromMessage(userText string) (name, phone string) {
	if m := recipientNameInlineRe.FindStringSubmatch(userText); len(m) > 1 {
		name = strings.TrimSpace(m[1])
	}
	if m := recipientPhoneInlineRe.FindStringSubmatch(userText); len(m) > 1 {
		phone = normalizePhoneID(m[1])
	}
	if name != "" || phone != "" {
		return name, phone
	}
	return parseRecipientLine(userText)
}

func hasRecipientHintInMessage(userText string) bool {
	name, phone := parseRecipientHintFromMessage(userText)
	text := strings.ToLower(strings.TrimSpace(userText))
	if phone != "" {
		return true
	}
	if name != "" && (strings.Contains(text, "nama:") || strings.Contains(text, "nama :") ||
		strings.Contains(text, "pembeli atas nama")) {
		return true
	}
	return false
}

func isSelfBuyerReference(text string) bool {
	for _, p := range selfBuyerLookupPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	if strings.Contains(text, "pembeli") && strings.Contains(text, "nama saya") {
		return true
	}
	return false
}

// IsSelfBuyerOrderLookup — cek pesanan milik chat ini (termasuk frasa "pembeli atas nama saya/ini").
func IsSelfBuyerOrderLookup(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || IsNewPurchaseIntentQuestion(userText) || IsOrderCancelRequest(userText) {
		return false
	}
	if parseOrderRefFromMessage(userText) != "" {
		return false
	}
	if isSelfBuyerReference(text) {
		return true
	}
	if hasRecipientHintInMessage(userText) && strings.Contains(text, "pembeli") {
		return true
	}
	if strings.Contains(text, "pembeli") &&
		(strings.Contains(text, "saya") || strings.Contains(text, "aku") || strings.Contains(text, " ini")) &&
		(strings.Contains(text, "?") || strings.Contains(text, "ada")) {
		return true
	}
	return false
}

// IsThirdPartyBuyerLookup — cari data pembeli/pesanan orang lain (bukan scoped ke chat ini).
func IsThirdPartyBuyerLookup(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if parseOrderRefFromMessage(userText) != "" || IsOrderStatusInquiry(userText) || IsSelfBuyerOrderLookup(userText) {
		return false
	}
	if isSelfBuyerReference(text) || hasRecipientHintInMessage(userText) {
		return false
	}
	for _, p := range thirdPartyBuyerLookupPhrases {
		if !strings.Contains(text, p) {
			continue
		}
		if p == "pembeli atas nama " || p == "customer atas nama" {
			rest := text[strings.Index(text, p)+len(p):]
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, "saya") || strings.HasPrefix(rest, "aku") ||
				strings.HasPrefix(rest, "ini") || strings.HasPrefix(rest, "saya ") {
				continue
			}
		}
		return true
	}
	if strings.Contains(text, "pembeli") && strings.Contains(text, "dengan nama") {
		return true
	}
	return false
}

func wantsOrderContextFromHistory(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	for _, p := range orderHistoryContextPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

func parseOrderRefFromHistory(history []Message) string {
	if len(history) == 0 {
		return ""
	}
	seen := 0
	for i := len(history) - 1; i >= 0 && seen < 8; i-- {
		if history[i].Direction != "out" && history[i].Direction != "system" {
			continue
		}
		seen++
		if ref := parseOrderRefFromMessage(history[i].Body); ref != "" {
			return ref
		}
	}
	return ""
}

func thirdPartyBuyerLookupDeniedReply() string {
	return "Maaf kak, saya hanya bisa bantu pesanan dari nomor WhatsApp ini. Untuk cek data pembeli lain, silakan hubungi tim toko ya."
}

func orderRecipientHintNotFoundReply() string {
	return "Tidak ada pesanan atas nama itu di chat ini ya kak. Sebut nomor pesanan (contoh WB-A1B2C3D4) atau tanya pesanan aktif Anda."
}

func parseOrderRefFromMessage(userText string) string {
	m := orderRefInMessageRe.FindStringSubmatch(userText)
	if len(m) < 2 {
		return ""
	}
	return "WB-" + strings.ToUpper(m[1])
}
