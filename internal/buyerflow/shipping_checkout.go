package buyerflow

import "strings"

var shippingFAQNegationWords = []string{
	"promo", "transfer", "bayar", "bukti", "ready", "restock", "stok",
	"rusak", "salah", "retur", "makasih",
}

// HasCheckoutDataInMessage — alamat, nama penerima, atau HP (data lanjut checkout).
func HasCheckoutDataInMessage(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if orderAddrHintRe.MatchString(text) || postalCodeIDRe.MatchString(text) {
		return true
	}
	if phoneIDRe.MatchString(text) {
		return true
	}
	if hasRecipientHintInMessage(userText) {
		return true
	}
	if strings.Contains(text, "nama:") || strings.Contains(text, "nama :") ||
		strings.Contains(text, "hp:") || strings.Contains(text, "hp :") {
		return true
	}
	_, phone := parseRecipientLine(userText)
	return phone != ""
}

func isShippingFAQNegated(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	for _, w := range shippingFAQNegationWords {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// IsCheckoutShippingFAQQuestion — shipping FAQ murni di tengah checkout (tanpa negasi / data checkout).
func IsCheckoutShippingFAQQuestion(userText string) bool {
	if !IsShippingFAQQuestion(userText) {
		return false
	}
	if isShippingFAQNegated(userText) {
		return false
	}
	if HasCheckoutDataInMessage(userText) {
		return false
	}
	return true
}

// TryCheckoutActiveShippingFAQReply — jawab ongkir/ETA tanpa clear cart; lanjutkan step aktif.
func TryCheckoutActiveShippingFAQReply(userText, step string, profile *BusinessProfile, kb []KBEntry, formal bool) (string, bool) {
	if !IsCheckoutShippingFAQQuestion(userText) {
		return "", false
	}
	var base string
	if IsShippingQuoteQuestion(userText) {
		base = checkoutActiveShippingQuoteBase(profile, kb, formal)
	} else if ans, ok := tryShippingKBFAQ(userText, kb); ok {
		base = ans
	} else {
		base = checkoutActiveShippingETABase(formal)
	}
	cta := checkoutShippingStepContinueCTA(step, formal)
	if cta == "" {
		return base, true
	}
	return strings.TrimSpace(base + "\n\n" + cta), true
}

func checkoutActiveShippingQuoteBase(profile *BusinessProfile, kb []KBEntry, formal bool) string {
	if ans, ok := tryFAQDirectAnswer("berapa ongkir", kb); ok {
		return ans
	}
	area := deliveryAreaLabel(profile)
	if formal {
		if area != "" {
			return "Ongkir dihitung setelah alamat lengkap dikonfirmasi. Area pengiriman kami: " + area + "."
		}
		return "Ongkir dihitung setelah alamat lengkap dikonfirmasi."
	}
	if area != "" {
		return "Ongkirnya nanti dihitung setelah alamat lengkap ya kak. Area kirim kami: " + area + "."
	}
	return "Ongkirnya nanti dihitung setelah alamat lengkap ya kak."
}

func checkoutActiveShippingETABase(formal bool) string {
	if formal {
		return "Estimasi sampainya tergantung kurir dan alamat tujuan ya kak. Tim kami yang konfirmasi setelah alamat lengkap."
	}
	return "Estimasi sampainya tergantung kurir dan alamat tujuan ya kak. Nanti tim kami konfirmasi setelah alamat lengkap."
}

func shippingETAGenericReply(formal bool) string {
	if formal {
		return "Estimasi sampainya tergantung kurir dan alamat tujuan ya kak. Tim kami konfirmasi begitu pesanan diproses."
	}
	return "Estimasi sampainya tergantung kurir dan alamat tujuan ya kak. Nanti tim kami kabari setelah pesanan diproses ya."
}

func checkoutShippingStepContinueCTA(step string, formal bool) string {
	switch step {
	case "ask_recipient":
		if formal {
			return "Mohon kirim nama penerima dan nomor HP terlebih dahulu."
		}
		return "Sekarang kirim nama penerima + no HP dulu ya."
	case "ask_address", "ask_address_full":
		if formal {
			return "Mohon kirim alamat lengkap pengiriman (jalan, RT/RW, kelurahan, kecamatan, kota, kode pos)."
		}
		return "Kirim alamat lengkap pengiriman ya kak (jalan, RT/RW, kelurahan, kecamatan, kota, kode pos)."
	default:
		return ""
	}
}
