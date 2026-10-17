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
		"delivery fee", "shipping fee", "same day", "ship today", "deliver today", "send today",
		"delivered today", "delivered tomorrow", "arrive today", "arrive tomorrow", "today or tomorrow",
		"hari ini atau besok", "sampai hari ini", "sampai besok", "same-day",
	}
	// "sampai besok" / "sampai hari ini" are also farewells ("oke kak, sampai besok ya"); they only
	// count as a shipping question when the message actually asks something.
	asks := strings.Contains(text, "?") || strings.Contains(text, "apakah") || strings.Contains(text, "bisa") ||
		strings.Contains(text, "kapan") || strings.Contains(text, "gak") || strings.Contains(text, "nggak")
	for _, s := range signals {
		if (s == "sampai besok" || s == "sampai hari ini") && !asks {
			continue
		}
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
	if ans, ok := tryFAQDirectAnswer(userText, kb); ok {
		return ans, true
	}
	if len(kb) > 0 {
		return BuildShippingFAQReply(userText, profile, kb, formal), true
	}
	if IsShippingQuoteOrFeeQuestion(userText) {
		return buildShippingQuoteReply(userText, profile, kb, formal), true
	}
	if ans, ok := tryShippingKBFAQ(userText, kb); ok {
		return LocalizeKBAnswerForBuyer(ans, userText), true
	}
	return buildShippingDefaultReply(userText, profile, formal), true
}

// sameDayShippingKBAnswer — "same day delivery?": the dispatch-hours rule from the KB first, then
// the ETA, in the buyer's language. Empty when the KB has no same-day rule (callers fall through).
func sameDayShippingKBAnswer(userText string, kb []KBEntry) string {
	facts := extractShippingKBFacts(kb)
	sameDay, eta := facts.sameDayID, facts.etaID
	if ResolveBuyerLanguage(userText, CurrentSavedBuyerLang()) == LangEN {
		sameDay, eta = facts.sameDayEN, facts.etaEN
	}
	if sameDay == "" {
		return ""
	}
	if eta == "" {
		return sameDay
	}
	return sameDay + " " + eta
}

func tryShippingKBFAQ(userText string, kb []KBEntry) (string, bool) {
	if asksSameDayDelivery(userText) {
		if ans := sameDayShippingKBAnswer(userText, kb); ans != "" {
			return ans, true
		}
	}
	if ans, ok := tryFAQDirectAnswer(userText, kb); ok {
		return ans, true
	}
	qTokens := tokenize(ExpandKBRetrievalQuery(userText))
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
		// An empty localization (Indonesian-only KB text for an English buyer) is "no KB answer",
		// never ("", true): the caller then uses its own localized template.
		if localized := strings.TrimSpace(LocalizeKBAnswerForBuyer(strings.TrimSpace(best.Answer), userText)); localized != "" {
			return localized, true
		}
	}
	return "", false
}

func buildShippingQuoteReply(userText string, profile *BusinessProfile, kb []KBEntry, formal bool) string {
	if len(kb) > 0 {
		return BuildShippingFAQReply(userText, profile, kb, formal)
	}
	if ans, ok := tryFAQDirectAnswer(userText, kb); ok {
		return LocalizeKBAnswerForBuyer(ans, userText)
	}
	area := DeliveryAreaForReply(deliveryAreaLabel(profile), ResolveBuyerLanguage(userText, CurrentSavedBuyerLang()))
	idFormalArea := "Ongkir dihitung setelah alamat lengkap dikonfirmasi. Area pengiriman kami: " + area +
		". Mohon kirim alamat lengkap (kecamatan/kota) agar kami bisa hitungkan ongkirnya."
	idFormal := "Ongkir dihitung setelah alamat lengkap dikonfirmasi. Mohon kirim alamat lengkap (kecamatan/kota) agar kami bisa hitungkan ongkirnya."
	idCasualArea := "Ongkirnya nanti dihitung setelah alamat lengkap ya kak. Area kirim kami: " + area +
		". Boleh kirim alamat lengkap (kecamatan/kota) biar bisa dihitung ongkirnya?"
	idCasual := "Ongkirnya nanti dihitung setelah alamat lengkap ya kak. Boleh kirim alamat lengkap (kecamatan/kota) biar bisa dihitung ongkirnya?"
	enFormalArea := "Shipping cost is calculated after we confirm your full address. " + enShipsTo("We deliver", area) +
		". Please send your full address (district/city) so we can quote shipping."
	enFormal := "Shipping cost is calculated after we confirm your full address. Please send your full address (district/city) so we can quote shipping."
	enCasualArea := "We'll calculate shipping after your full address is confirmed. " + enShipsTo("We ship", area) +
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
	area := DeliveryAreaForReply(deliveryAreaLabel(profile), ResolveBuyerLanguage(userText, CurrentSavedBuyerLang()))
	base := shippingETAGenericReplyFor(userText, formal)
	if area == "" {
		return base
	}
	idFormal := "Kami melayani pengiriman ke " + area + ". " + base
	idCasual := "Kami kirim ke " + area + " ya kak. " + base
	enFormal := enShipsTo("We deliver", area) + ". " + base
	enCasual := enShipsTo("We ship", area) + ". " + base
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
