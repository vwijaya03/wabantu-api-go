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
		base = checkoutActiveShippingQuoteBase(userText, profile, kb, formal)
	} else if ans, ok := tryShippingKBFAQ(userText, kb); ok && strings.TrimSpace(ans) != "" {
		base = ans
	} else {
		base = checkoutActiveShippingETABase(userText, formal)
	}
	cta := checkoutShippingStepContinueCTA(userText, step, formal)
	if cta == "" {
		return base, true
	}
	return strings.TrimSpace(base + "\n\n" + cta), true
}

func checkoutActiveShippingQuoteBase(userText string, profile *BusinessProfile, kb []KBEntry, formal bool) string {
	if ans, ok := tryFAQDirectAnswer("berapa ongkir", kb); ok {
		return ans
	}
	area := deliveryAreaLabel(profile)
	idFormalArea := "Ongkir dihitung setelah alamat lengkap dikonfirmasi. Area pengiriman kami: " + area + "."
	idFormal := "Ongkir dihitung setelah alamat lengkap dikonfirmasi."
	idCasualArea := "Ongkirnya nanti dihitung setelah alamat lengkap ya kak. Area kirim kami: " + area + "."
	idCasual := "Ongkirnya nanti dihitung setelah alamat lengkap ya kak."
	enFormalArea := "Shipping is calculated after we confirm your full address. We deliver to: " + area + "."
	enFormal := "Shipping is calculated after we confirm your full address."
	enCasualArea := "We'll calculate shipping after your full address is confirmed. We ship to: " + area + "."
	enCasual := "We'll calculate shipping after your full address is confirmed."
	if formal {
		if area != "" {
			return PickBuyerLanguageReply(userText, formal, idFormalArea, enFormalArea)
		}
		return PickBuyerLanguageReply(userText, formal, idFormal, enFormal)
	}
	if area != "" {
		return PickBuyerLanguageReply(userText, formal, idCasualArea, enCasualArea)
	}
	return PickBuyerLanguageReply(userText, formal, idCasual, enCasual)
}

func checkoutActiveShippingETABase(userText string, formal bool) string {
	return shippingETAGenericReplyFor(userText, formal)
}

func shippingETAGenericReply(formal bool) string {
	return shippingETAGenericReplyFor("", formal)
}

func shippingETAGenericReplyFor(userText string, formal bool) string {
	idFormal := "Estimasi sampainya tergantung kurir dan alamat tujuan ya kak. Tim kami konfirmasi begitu pesanan diproses."
	idCasual := "Estimasi sampainya tergantung kurir dan alamat tujuan ya kak. Nanti tim kami kabari setelah pesanan diproses ya."
	enFormal := "Delivery timing depends on the courier and destination. Our team confirms once your order is processed."
	enCasual := "Delivery timing depends on the courier and your address. We'll update you after the order is processed."
	if formal {
		return PickBuyerLanguageReply(userText, formal, idFormal, enFormal)
	}
	return PickBuyerLanguageReply(userText, formal, idCasual, enCasual)
}

func checkoutShippingStepContinueCTA(userText, step string, formal bool) string {
	switch step {
	case "ask_recipient":
		idFormal := "Mohon kirim nama penerima dan nomor HP terlebih dahulu."
		idCasual := "Sekarang kirim nama penerima + no HP dulu ya."
		enFormal := "Please send the recipient's name and mobile number first."
		enCasual := "Next, please send the recipient's name and phone number."
		if formal {
			return PickBuyerLanguageReply(userText, formal, idFormal, enFormal)
		}
		return PickBuyerLanguageReply(userText, formal, idCasual, enCasual)
	case "ask_address", "ask_address_full":
		idFormal := "Mohon kirim alamat lengkap pengiriman (jalan, RT/RW, kelurahan, kecamatan, kota, kode pos)."
		idCasual := "Kirim alamat lengkap pengiriman ya kak (jalan, RT/RW, kelurahan, kecamatan, kota, kode pos)."
		enFormal := "Please send the full shipping address (street, district, city, postal code)."
		enCasual := "Please send your full shipping address (street, district, city, postal code)."
		if formal {
			return PickBuyerLanguageReply(userText, formal, idFormal, enFormal)
		}
		return PickBuyerLanguageReply(userText, formal, idCasual, enCasual)
	default:
		return ""
	}
}
