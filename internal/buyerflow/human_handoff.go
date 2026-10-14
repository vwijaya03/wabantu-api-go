package buyerflow

import (
	"regexp"
	"strings"
)

var humanHandoffExplicit = []string{
	"minta cs", "minta ke cs", "minta customer service",
	"hubungi admin", "hubungkan ke admin", "hubung ke admin",
	"chat cs", "chat sama cs", "chat dengan cs",
	"mau chat sama cs", "mau ngobrol sama cs",
	"bicara dengan manusia", "bicara sama manusia", "bicara ke manusia",
	"sambungkan ke manusia", "sambungkan ke cs", "sambung ke cs",
	"tolong sambungkan ke cs", "tolong hubungkan ke cs",
	"hubungkan ke customer service", "hubungi customer service", "sambungkan ke customer service",
	"connect ke cs", "hubungkan ke cs", "hubung ke cs",
	"tolong cs", "panggil cs", "panggilkan cs",
}

var humanHandoffVerbRe = regexp.MustCompile(`(?i)\b(minta|tolong|mau|bisa|boleh|hubung|sambung|chat|bicara|ngobrol|panggil|connect|terhubung)\b`)
var humanSupportTargetRe = regexp.MustCompile(`(?i)\b(manusia|operator|agent|admin)\b|customer service|\bcs\b`)
var customerServiceInfoCueRe = regexp.MustCompile(`(?i)\b(jam|buka|tutup|operasional|hours|open|close|kontak|nomor|telepon|whatsapp|email)\b`)

// HumanHandoffReply — singkat, sopan; tidak memulai order baru.
func HumanHandoffReply(formal bool) string {
	if formal {
		return "Baik kak, permintaan kakak sudah kami teruskan ke tim CS. Draft pesanan kakak tetap kami simpan; tim kami akan melanjutkan chat ini segera 🙏"
	}
	return "Siap kak, aku sambungkan ke tim CS ya. Draft pesanan kakak tetap aman; tim kami lanjutin chat ini sebentar lagi 🙏"
}

// IsHumanEscalationRequest — permintaan jelas ke CS/manusia, bukan teks checkout/ongkir/produk kebetulan.
func IsHumanEscalationRequest(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if humanEscalationExcludedContext(userText, text) {
		return false
	}
	for _, phrase := range humanHandoffExplicit {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	if humanHandoffVerbRe.MatchString(text) && humanSupportTargetRe.MatchString(text) {
		return true
	}
	if humanSupportTargetRe.MatchString(text) &&
		(strings.Contains(text, "dong") || strings.Contains(text, "please") ||
			strings.HasSuffix(strings.TrimSpace(text), "cs") || strings.HasPrefix(text, "cs ")) {
		return true
	}
	return false
}

func humanEscalationExcludedContext(userText, text string) bool {
	if IsRecipientPolicyQuestion(userText) {
		return true
	}
	if isCustomerServiceInfoQuestion(text) {
		return true
	}
	if IsStructuredOrderList(userText) || IsExplicitNewOrderStart(userText) {
		return true
	}
	if IsShippingQuoteQuestion(userText) && !humanSupportTargetRe.MatchString(text) {
		return true
	}
	if IsPaymentQuestion(userText) && !humanSupportTargetRe.MatchString(text) {
		return true
	}
	if IsMinimumOrderQuestion(userText) && !humanSupportTargetRe.MatchString(text) {
		return true
	}
	if IsStoreLocationQuestion(userText) && !humanSupportTargetRe.MatchString(text) {
		return true
	}
	if orderAddrHintRe.MatchString(text) && !humanHandoffVerbRe.MatchString(text) && !humanSupportTargetRe.MatchString(text) {
		return true
	}
	if postalCodeIDRe.MatchString(text) && !humanSupportTargetRe.MatchString(text) {
		return true
	}
	if phoneIDRe.MatchString(text) && !humanSupportTargetRe.MatchString(text) {
		return true
	}
	if strings.Contains(text, "ongkir") && !humanSupportTargetRe.MatchString(text) {
		return true
	}
	if strings.Contains(text, "promo") && !humanSupportTargetRe.MatchString(text) {
		return true
	}
	return false
}

var humanHandoffEscalationVerbRe = regexp.MustCompile(`(?i)\b(hubung|sambung|panggil|minta|connect|terhubung)\b`)

// isCustomerServiceInfoQuestion — tanya jam/kontak CS, bukan minta dihubungkan ke agent.
func isCustomerServiceInfoQuestion(text string) bool {
	if !strings.Contains(text, "customer service") && !humanSupportTargetRe.MatchString(text) {
		return false
	}
	if humanHandoffEscalationVerbRe.MatchString(text) {
		return false
	}
	return customerServiceInfoCueRe.MatchString(text) || strings.Contains(text, "jam berapa")
}
