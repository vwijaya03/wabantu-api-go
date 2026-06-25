package ai

import "strings"

var recipientPolicyPhrases = []string{
	"atas nama orang lain",
	"beli untuk orang lain",
	"pesan untuk orang lain",
	"pesan buat orang lain",
	"beli buat orang lain",
	"kirim untuk orang lain",
	"kirim ke orang lain",
	"beli atas nama orang",
	"pesan atas nama orang",
	"order untuk orang lain",
	"order buat orang lain",
}

// IsRecipientPolicyQuestion — tanya kebijakan pesan/beli atas nama penerima lain (bukan lookup order).
func IsRecipientPolicyQuestion(userText string) bool {
	text := normalizeBuyerTextForRules(userText)
	if text == "" {
		return false
	}
	if parseOrderRefFromMessage(userText) != "" {
		return false
	}
	// Jangan panggil IsSelfBuyerOrderLookup / IsThirdPartyBuyerLookup — mereka memanggil
	// IsNewPurchaseIntentQuestion → IsConsultingPurchaseQuestion → rekursi ke sini.
	if isSelfBuyerReference(text) || hasRecipientHintInMessage(userText) {
		return false
	}
	for _, p := range orderStatusInquiryPhrases {
		if strings.Contains(text, p) {
			return false
		}
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
		return false
	}
	if strings.Contains(text, "pembeli") && strings.Contains(text, "dengan nama") {
		return false
	}
	// Lookup: "pembeli atas nama X ada?"
	if strings.Contains(text, "pembeli") && strings.Contains(text, "ada") {
		return false
	}
	for _, p := range recipientPolicyPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	if strings.Contains(text, "atas nama") &&
		(strings.Contains(text, "orang lain") || strings.Contains(text, "penerima lain")) {
		return true
	}
	if (strings.Contains(text, "bisa") || strings.Contains(text, "boleh")) &&
		strings.Contains(text, "atas nama") &&
		!strings.Contains(text, "nama saya") && !strings.Contains(text, "nama aku") &&
		!strings.Contains(text, "nama ini") && !strings.Contains(text, "nama ku") {
		if strings.Contains(text, "beli") || strings.Contains(text, "pesan") || strings.Contains(text, "order") {
			return true
		}
	}
	return false
}

func buildRecipientPolicyReply(formal bool) string {
	if formal {
		return "Bisa kak. Pesanan atas nama orang lain diperbolehkan. " +
			"Saat proses pesanan, kami akan meminta nama dan nomor HP/WA penerima."
	}
	return "Bisa kak, pesan atas nama orang lain boleh. " +
		"Nanti pas proses pesanan saya minta nama & nomor HP/WA penerima ya."
}

func replyRecipientPolicyQuestion(userText string, kb []dbKBEntry, formal bool) string {
	if ans, ok := tryFAQDirectAnswer(userText, kb); ok {
		return ans
	}
	return buildRecipientPolicyReply(formal)
}
