package buyerflow

import (
	"regexp"
	"strings"
)

var humanHandoffExplicit = []string{
	"talk to a human", "speak to a human", "speak with a human", "talk with a human",
	"talk to customer service", "speak to customer service", "speak with customer service",
	"connect me to a human", "connect me to customer service", "connect to customer service",
	"i need a human", "real person please", "live agent please",
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

const (
	// humanTalkVerbPat — verbs that mean "put me through to someone". The generic
	// minta/tolong/mau/bisa/boleh live only in humanHandoffVerbRe: next to a plain noun
	// ("mau beli human hair", "bisa untuk 3 orang") they are not a request for staff.
	humanTalkVerbPat = `hubung(?:kan|i)?|sambung(?:kan)?|chat|bicara|ngobrol|panggil(?:kan)?|connect|terhubung|contact|talk|speak|call|reach|complain`
	// humanCoreTargetPat — support words that are a target on their own ("cs dong", "admin please").
	humanCoreTargetPat = `\b(manusia|operator|agent|admin)\b|customer service|\bcs\b`
	// humanRoleTargetPat — staff roles; a target together with any request verb.
	humanRoleTargetPat = `\b(administrator|owner|pemilik|manager|manajer|staff|staf)\b|real person`
	// humanGenericTargetPat — plain nouns (headcounts, products); a target only with a talk verb.
	humanGenericTargetPat = `\b(person|human|orang|penjual|seller)\b`
)

var humanHandoffVerbRe = regexp.MustCompile(`(?i)\b(minta|tolong|mau|bisa|boleh|` + humanTalkVerbPat + `)\b`)
var humanTalkVerbRe = regexp.MustCompile(`(?i)\b(` + humanTalkVerbPat + `)\b`)
var humanCoreTargetRe = regexp.MustCompile(`(?i)` + humanCoreTargetPat)
var humanNamedTargetRe = regexp.MustCompile(`(?i)` + humanCoreTargetPat + `|` + humanRoleTargetPat)
var humanGenericTargetRe = regexp.MustCompile(`(?i)` + humanGenericTargetPat)

// humanSupportTargetRe — every target word; the exclusion helpers below use it unchanged.
var humanSupportTargetRe = regexp.MustCompile(`(?i)` + humanCoreTargetPat + `|` + humanRoleTargetPat + `|` + humanGenericTargetPat)
var customerServiceInfoCueRe = regexp.MustCompile(`(?i)\b(jam|buka|tutup|operasional|hours|open|close|kontak|nomor|telepon|whatsapp|email)\b`)

// HumanHandoffReply — singkat, sopan; tidak memulai order baru.
func HumanHandoffReply(formal bool) string {
	if formal {
		return "Baik kak, permintaan kakak sudah kami teruskan ke tim CS. Draft pesanan kakak tetap kami simpan; tim kami akan melanjutkan chat ini segera 🙏"
	}
	return "Siap kak, aku sambungkan ke tim CS ya. Draft pesanan kakak tetap aman; tim kami lanjutin chat ini sebentar lagi 🙏"
}

// HumanHandoffReplyFor picks Indonesian or English copy from the buyer message language.
func HumanHandoffReplyFor(userText string, formal bool) string {
	idFormal := "Baik kak, permintaan kakak sudah kami teruskan ke tim CS. Draft pesanan kakak tetap kami simpan; tim kami akan melanjutkan chat ini segera 🙏"
	idCasual := "Siap kak, aku sambungkan ke tim CS ya. Draft pesanan kakak tetap aman; tim kami lanjutin chat ini sebentar lagi 🙏"
	enFormal := "Got it — I've passed your request to our team. Your order draft stays saved; a teammate will continue this chat shortly 🙏"
	enCasual := "Sure — I'll connect you with our team. Your order draft is safe; someone will continue this chat shortly 🙏"
	if formal {
		return PickBuyerLanguageReply(userText, formal, idFormal, enFormal)
	}
	return PickBuyerLanguageReply(userText, formal, idCasual, enCasual)
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
	if humanHandoffVerbRe.MatchString(text) &&
		(humanNamedTargetRe.MatchString(text) ||
			(humanGenericTargetRe.MatchString(text) && humanTalkVerbRe.MatchString(text))) {
		return true
	}
	if humanCoreTargetRe.MatchString(text) &&
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
