package buyerflow

import (
	"regexp"
	"strings"
)

// Explicit buyer confirmation of the final order summary (the "confirm" checkout step of the
// LLM-first pipeline). The legacy FSM may only finish an order from that step when the buyer
// really approved it: any other text keeps the draft and asks again. Fail-closed.

var confirmPunctRe = regexp.MustCompile(`[^\p{L}\p{N}' ]+`)

// confirmAffirmTokens carry the approval itself; a message needs at least one of them.
var confirmAffirmTokens = map[string]bool{
	"ok": true, "oke": true, "okey": true, "okay": true, "okeh": true, "okk": true, "okee": true,
	"ya": true, "iya": true, "yaa": true, "yap": true, "yup": true, "yes": true, "yeah": true, "y": true,
	"benar": true, "bener": true, "betul": true, "correct": true, "right": true,
	"setuju": true, "lanjut": true, "lanjutkan": true, "proses": true, "process": true, "proceed": true,
	"confirm": true, "confirmed": true, "konfirmasi": true, "deal": true, "sip": true, "siap": true,
	"gas": true, "sure": true,
}

// confirmFillerTokens may accompany an approval ("ok kak, sudah benar ya") but never approve alone.
var confirmFillerTokens = map[string]bool{
	"kak": true, "ka": true, "kakak": true, "dong": true, "deh": true, "aja": true, "saja": true, "nih": true,
	"sih": true, "banget": true, "sekali": true, "please": true, "pls": true, "tolong": true,
	"silakan": true, "silahkan": true, "sudah": true, "udah": true, "itu": true, "ini": true, "semua": true,
	"sama": true, "yang": true, "the": true, "it": true, "is": true, "that": true, "that's": true, "thats": true,
	"all": true, "thanks": true, "thank": true, "you": true, "makasih": true, "terima": true, "kasih": true,
	"lah": true, "mantap": true, "datanya": true, "data": true, "pesanan": true, "order": true,
	"my": true, "i": true, "am": true, "and": true, "dan": true, "kok": true, "kan": true,
}

// confirmPhrases are approvals whose words are too ambiguous to sit in the token sets.
var confirmPhrases = map[string]bool{
	"looks good": true, "look good": true, "sounds good": true, "all good": true, "go ahead": true,
	"it's good": true, "its good": true, "sudah oke": true, "udah oke": true, "sudah ok": true,
	"udah ok": true, "gas aja": true, "yes please": true, "ok proses": true, "oke proses": true,
	"sudah sesuai": true, "udah sesuai": true, "sesuai": true,
}

// maxConfirmTokens: an approval is short. A long message that happens to contain "ok" is not one.
const maxConfirmTokens = 8

var shippingAmendIntentRe = regexp.MustCompile(`(?i)\b(ganti|ubah|salah)\s+(alamat|address)\b`)

// IsShippingAmendIntent reports that the buyer wants to change delivery details (with or without
// the new address in the same message). Used at the confirm step before fail-closed re-prompt.
func IsShippingAmendIntent(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return shippingAmendIntentRe.MatchString(lower) || strings.Contains(lower, "ganti alamat") || strings.Contains(lower, "ubah alamat")
}

// IsExplicitOrderConfirm reports whether text is the buyer's plain approval of the order summary
// ("ok", "ya benar kak", "lanjut proses", "looks good", "👍"). It is deliberately strict: every word
// must be an approval or a filler, so "ok tapi ganti alamat", "tidak" or "ok berapa ongkirnya?"
// are not confirmations. A question never confirms.
func IsExplicitOrderConfirm(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.Contains(trimmed, "?") {
		return false
	}
	switch trimmed {
	case "👍", "👌", "✅", "👍🏻", "👍🏼", "👍🏽":
		return true
	}
	cleaned := strings.Join(strings.Fields(confirmPunctRe.ReplaceAllString(strings.ToLower(trimmed), " ")), " ")
	if cleaned == "" {
		return false
	}
	if confirmPhrases[cleaned] {
		return true
	}
	tokens := strings.Fields(cleaned)
	if len(tokens) > maxConfirmTokens {
		return false
	}
	affirmed := false
	for _, tok := range tokens {
		switch {
		case confirmAffirmTokens[tok]:
			affirmed = true
		case confirmFillerTokens[tok]:
		default:
			return false
		}
	}
	return affirmed
}

// orderReconfirmReply shows the order again and asks for the OK. English or Indonesian follows
// the buyer's language (current message, then the saved preference).
func orderReconfirmReply(st OrderState, userText string) string {
	en := ResolveBuyerLanguage(userText, CurrentSavedBuyerLang()) == LangEN
	ask := "Kalau sudah benar, balas OK ya Kak biar aku proses. Kalau ada yang mau diubah, bilang saja."
	if en {
		ask = "If everything looks right, reply OK and I'll process it. If you want to change anything, just tell me."
	}
	if summary := formatOrderSummary(st); summary != "" {
		return summary + "\n\n" + ask
	}
	return ask
}
