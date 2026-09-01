package buyerflow

import "strings"

// IsShippingFAQQuestion — ongkir, estimasi kirim, wilayah pengiriman (bukan langkah order).
func IsShippingFAQQuestion(userText string) bool {
	if IsShippingQuoteQuestion(userText) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	signals := []string{
		"pengiriman", "ongkir", "ongkos kirim", "kirim ke",
		"estimasi sampai", "berapa lama", "lama sampai", "waktu kirim",
		"luar kota", "wilayah pengiriman", "area kirim", "delivery",
		"bisa kirim", "kirim nggak", "kirim gak", "kirim tidak",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
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
	if ans, ok := tryShippingKBFAQ(userText, kb); ok {
		return ans, true
	}
	return buildShippingDefaultReply(profile, formal), true
}

func tryShippingKBFAQ(userText string, kb []KBEntry) (string, bool) {
	if ans, ok := tryFAQDirectAnswer(userText, kb); ok {
		return ans, true
	}
	qTokens := tokenize(userText)
	var best KBEntry
	var bestScore float64
	for _, entry := range kb {
		cat := ""
		if entry.Category != nil {
			cat = *entry.Category
		}
		if !strings.Contains(strings.ToLower(cat), "pengiriman") && !strings.Contains(strings.ToLower(cat), "shipping") {
			continue
		}
		text := entry.Question + " " + entry.Answer
		score := overlapScore(qTokens, tokenize(text))
		if score > bestScore {
			bestScore = score
			best = entry
		}
	}
	if bestScore >= 0.35 && strings.TrimSpace(best.Answer) != "" {
		return strings.TrimSpace(best.Answer), true
	}
	return "", false
}

func buildShippingQuoteReply(userText string, profile *BusinessProfile, kb []KBEntry, formal bool) string {
	if ans, ok := tryFAQDirectAnswer(userText, kb); ok {
		return ans
	}
	area := deliveryAreaLabel(profile)
	if formal {
		if area != "" {
			return "Ongkir dihitung setelah alamat lengkap dikonfirmasi. Area pengiriman kami: " + area +
				". Mohon kirim alamat lengkap (kecamatan/kota) agar kami bisa hitungkan ongkirnya."
		}
		return "Ongkir dihitung setelah alamat lengkap dikonfirmasi. Mohon kirim alamat lengkap (kecamatan/kota) agar kami bisa hitungkan ongkirnya."
	}
	if area != "" {
		return "Ongkirnya nanti dihitung setelah alamat lengkap ya kak. Area kirim kami: " + area +
			". Boleh kirim alamat lengkap (kecamatan/kota) biar bisa dihitung ongkirnya?"
	}
	return "Ongkirnya nanti dihitung setelah alamat lengkap ya kak. Boleh kirim alamat lengkap (kecamatan/kota) biar bisa dihitung ongkirnya?"
}

func buildShippingDefaultReply(profile *BusinessProfile, formal bool) string {
	area := deliveryAreaLabel(profile)
	if formal {
		if area != "" {
			return "Kami melayani pengiriman ke " + area + ". Untuk estimasi ongkir, mohon sertakan alamat lengkap (kecamatan/kota)."
		}
		return "Kami melayani pengiriman. Untuk estimasi ongkir, mohon sertakan alamat lengkap (kecamatan/kota)."
	}
	if area != "" {
		return "Kami kirim ke " + area + " ya kak. Kalau mau hitung ongkir, kirim alamat lengkap (kecamatan/kota) aja."
	}
	return "Kami layani pengiriman ya kak. Kalau mau hitung ongkir, kirim alamat lengkap (kecamatan/kota) aja."
}

func deliveryAreaLabel(profile *BusinessProfile) string {
	if profile == nil || profile.DeliveryArea == nil {
		return ""
	}
	return strings.TrimSpace(*profile.DeliveryArea)
}
