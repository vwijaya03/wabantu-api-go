package buyerflow

import "strings"

// IsShippingFAQQuestion — ongkir, estimasi kirim, wilayah pengiriman (bukan langkah order).
func IsShippingFAQQuestion(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if IsShippingQuoteQuestion(userText) {
		return !isShippingFAQNegated(text)
	}
	if text == "" {
		return false
	}
	signals := []string{
		"pengiriman", "ongkir", "ongkos kirim", "kirim ke",
		"estimasi sampai", "estimasi pengiriman", "berapa lama", "berapa hari", "lama sampai", "waktu kirim",
		"kapan sampai", "sampai kapan", "sampai dirumah", "sampai di rumah", "sampai rumah", "barang sampai",
		"kapan barang", "lama kirim",
		"luar kota", "wilayah pengiriman", "area kirim", "delivery",
		"bisa kirim", "kirim nggak", "kirim gak", "kirim tidak",
		"how long is shipping", "how long does shipping", "how many days until",
		"when will it arrive", "when will my order arrive", "shipping time", "delivery time",
		"how long until it arrives", "days until it arrives",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			if isShippingFAQNegated(text) {
				return false
			}
			return true
		}
	}
	return false
}

// TryShippingFAQReply answers shipping questions from KB or deterministic templates (no RajaOngkir v1).
func TryShippingFAQReply(userText string, profile *BusinessProfile, kb []KBEntry, formal bool) (string, bool) {
	if !IsShippingFAQQuestion(userText) {
		return "", false
	}
	if IsShippingQuoteQuestion(userText) {
		return buildShippingQuoteReply(userText, profile, kb, formal), true
	}
	if ans, ok := tryFAQDirectAnswer(userText, kb); ok {
		return ans, true
	}
	if ans, ok := tryShippingKBFAQ(userText, kb); ok {
		return ans, true
	}
	return buildShippingDefaultReply(userText, profile, formal), true
}

func tryShippingKBFAQ(userText string, kb []KBEntry) (string, bool) {
	if ans, ok := tryFAQDirectAnswer(userText, kb); ok {
		return ans, true
	}
	qTokens := tokenize(userText)
	var best KBEntry
	var bestScore float64
	minScore := 0.35
	if IsEnglishBuyerMessage(userText) {
		minScore = 0.12
	}
	for _, entry := range kb {
		cat := ""
		if entry.Category != nil {
			cat = *entry.Category
		}
		catLower := strings.ToLower(cat)
		if !strings.Contains(catLower, "pengiriman") && !strings.Contains(catLower, "shipping") &&
			!strings.Contains(catLower, "delivery") {
			continue
		}
		text := entry.Question + " " + entry.Answer
		score := overlapScore(qTokens, tokenize(text))
		if score > bestScore {
			bestScore = score
			best = entry
		}
	}
	if bestScore >= minScore && strings.TrimSpace(best.Answer) != "" {
		return strings.TrimSpace(best.Answer), true
	}
	return "", false
}

func buildShippingQuoteReply(userText string, profile *BusinessProfile, kb []KBEntry, formal bool) string {
	if ans, ok := tryFAQDirectAnswer(userText, kb); ok {
		return ans
	}
	area := deliveryAreaLabel(profile)
	idFormalArea := "Ongkir dihitung setelah alamat lengkap dikonfirmasi. Area pengiriman kami: " + area +
		". Mohon kirim alamat lengkap (kecamatan/kota) agar kami bisa hitungkan ongkirnya."
	idFormal := "Ongkir dihitung setelah alamat lengkap dikonfirmasi. Mohon kirim alamat lengkap (kecamatan/kota) agar kami bisa hitungkan ongkirnya."
	idCasualArea := "Ongkirnya nanti dihitung setelah alamat lengkap ya kak. Area kirim kami: " + area +
		". Boleh kirim alamat lengkap (kecamatan/kota) biar bisa dihitung ongkirnya?"
	idCasual := "Ongkirnya nanti dihitung setelah alamat lengkap ya kak. Boleh kirim alamat lengkap (kecamatan/kota) biar bisa dihitung ongkirnya?"
	enFormalArea := "Shipping cost is calculated after we confirm your full address. We deliver to: " + area +
		". Please send your full address (district/city) so we can quote shipping."
	enFormal := "Shipping cost is calculated after we confirm your full address. Please send your full address (district/city) so we can quote shipping."
	enCasualArea := "We'll calculate shipping after your full address is confirmed. We ship to: " + area +
		". Could you send your full address (district/city) so we can quote shipping?"
	enCasual := "We'll calculate shipping after your full address is confirmed. Could you send your full address (district/city) so we can quote shipping?"
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

func buildShippingDefaultReply(userText string, profile *BusinessProfile, formal bool) string {
	area := deliveryAreaLabel(profile)
	base := shippingETAGenericReplyFor(userText, formal)
	if area == "" {
		return base
	}
	idFormal := "Kami melayani pengiriman ke " + area + ". " + base
	idCasual := "Kami kirim ke " + area + " ya kak. " + base
	enFormal := "We deliver to " + area + ". " + base
	enCasual := "We ship to " + area + ". " + base
	if formal {
		return PickBuyerLanguageReply(userText, formal, idFormal, enFormal)
	}
	return PickBuyerLanguageReply(userText, formal, idCasual, enCasual)
}

func deliveryAreaLabel(profile *BusinessProfile) string {
	if profile == nil || profile.DeliveryArea == nil {
		return ""
	}
	return strings.TrimSpace(*profile.DeliveryArea)
}
