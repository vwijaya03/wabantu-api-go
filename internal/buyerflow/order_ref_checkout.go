package buyerflow

import (
	"sort"
	"strings"
)

// ParseAllOrderRefsFromMessage returns unique WB- refs in message order.
func ParseAllOrderRefsFromMessage(userText string) []string {
	matches := orderRefInMessageRe.FindAllStringSubmatch(userText, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		ref := "WB-" + strings.ToUpper(m[1])
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

// OrderRefMatchesPersistedID — cocokkan ref chat dengan UUID order tersimpan.
func OrderRefMatchesPersistedID(ref, persistedOrderID string) bool {
	if ref == "" || strings.TrimSpace(persistedOrderID) == "" {
		return false
	}
	return strings.EqualFold(ref, FormatOrderNumber(persistedOrderID))
}

// IsOrderRefCheckoutContinuation — WB-ref + data checkout; bukan status/amend/merge/bukti.
func IsOrderRefCheckoutContinuation(userText string) bool {
	if parseOrderRefFromMessage(userText) == "" {
		return false
	}
	if !HasCheckoutDataInMessage(userText) {
		return false
	}
	if IsOrderStatusInquiry(userText) || IsOrderRefStatusLookup(userText) {
		return false
	}
	if IsOrderAmendMessage(userText) || IsCheckoutMergeIntent(userText) {
		return false
	}
	if IsCartLineCorrectionIntent(userText) {
		return false
	}
	if IsPaymentStatusInquiry(userText) {
		return false
	}
	return true
}

// OrderRefCheckoutPickOneReply — multi-ref tanpa write DB.
func OrderRefCheckoutPickOneReply(refs []string, formal bool) string {
	sorted := append([]string(nil), refs...)
	sort.Strings(sorted)
	var b strings.Builder
	if formal {
		b.WriteString("Ada beberapa nomor pesanan di pesan ini. Sebut satu nomor yang mau dilanjutkan ya:\n\n")
	} else {
		b.WriteString("Ada beberapa nomor pesanan nih kak. Pilih satu nomor yang mau dilanjutkan ya:\n\n")
	}
	for i, ref := range sorted {
		b.WriteString(strings.TrimSpace(ref))
		if i < len(sorted)-1 {
			b.WriteString(" / ")
		}
	}
	return strings.TrimSpace(b.String())
}

func orderRefCheckoutNotFoundReply(ref string) string {
	if ref == "" {
		return "Nomor pesanan tidak ditemukan di chat ini. Pastikan WB-XXXX dari pesanan kamu di percakapan ini ya kak."
	}
	return "Nomor pesanan " + ref + " tidak ditemukan di chat ini. Cek lagi nomornya ya kak."
}

func orderRefCheckoutAccessDeniedReply() string {
	return "Maaf kak, pesanan itu bukan dari nomor WhatsApp ini. Saya hanya bisa bantu pesanan di chat ini ya."
}

func orderStatusLabelIDSimple(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "draft":
		return "draft"
	case "processing", "confirmed":
		return "diproses"
	case "paid":
		return "sudah dibayar"
	case "shipped":
		return "dikirim"
	case "completed":
		return "selesai"
	case "cancelled":
		return "dibatalkan"
	default:
		return strings.TrimSpace(status)
	}
}

// OrderRefCheckoutNonDraftReply — order sudah lewat draft; tidak buka checkout.
func OrderRefCheckoutNonDraftReply(formal bool, status, ref string) string {
	label := orderStatusLabelIDSimple(status)
	if formal {
		return "Maaf kak, pesanan " + ref + " sudah " + label + " jadi data pengiriman tidak bisa diubah lewat chat. Tim CS bisa bantu kalau perlu."
	}
	return "Maaf kak, pesanan " + ref + " sudah " + label + " jadi data pengiriman belum bisa diubah lewat chat 🙏"
}
