package buyerflow

import (
	"regexp"
	"strings"
)

// accountNumberRe — a run of 6+ digits: the KB entry that actually carries bank account numbers.
var accountNumberRe = regexp.MustCompile(`\d{6,}`)

// asksWhereToPay — the buyer asks where/how to send the money, so the KB entry with the account
// numbers must beat the generic "we accept transfer and QRIS" entry.
func asksWhereToPay(query string) bool {
	q := strings.ToLower(query)
	for _, s := range []string{
		"rekening", "transfer ke", "bayar ke mana", "kemana",
		"where", "which account", "account number", "send payment", "how do i pay", "how to pay",
	} {
		if strings.Contains(q, s) {
			return true
		}
	}
	return false
}

func isPaymentKBEntry(question, category, answer string) bool {
	question = strings.ToLower(strings.TrimSpace(question))
	category = strings.ToLower(strings.TrimSpace(category))
	answer = strings.ToLower(answer)
	if category == "pembayaran" || strings.Contains(category, "payment") {
		return true
	}
	// FAQ pengiriman often mentions "pembayaran" in the answer — not payment instructions.
	if category == "pengiriman" || strings.Contains(category, "ship") ||
		category == "retur" || category == "order" {
		return false
	}
	tags := []string{"payment", "rekening", "bank", "transfer", "pembayaran", "qris", "cod"}
	for _, t := range tags {
		if strings.Contains(question, t) || strings.Contains(category, t) {
			return true
		}
	}
	for _, t := range tags {
		if strings.Contains(answer, t) {
			return true
		}
	}
	return false
}

func paymentKBEntryScore(query string, e KBEntry) float64 {
	q := strings.ToLower(strings.TrimSpace(query))
	text := strings.ToLower(e.Question + " " + e.Answer)
	score := overlapScore(tokenize(q), tokenize(text))
	cat := ""
	if e.Category != nil {
		cat = strings.ToLower(*e.Category)
	}
	if cat == "pembayaran" || strings.Contains(cat, "payment") {
		score += 0.35
	}
	paymentIntent := strings.Contains(q, "rekening") || strings.Contains(q, "bank") ||
		strings.Contains(q, "transfer") || strings.Contains(q, "pay") || strings.Contains(q, "payment") ||
		strings.Contains(q, "bayar") || strings.Contains(q, "pembayaran") || strings.Contains(q, "cara bayar")
	if paymentIntent {
		if strings.Contains(text, "bca") || strings.Contains(text, "rekening") ||
			strings.Contains(text, "bank transfer") || strings.Contains(text, "transfer bank") {
			score += 0.4
		}
	}
	return score
}

func paymentKBFallbackPriority(e KBEntry) int {
	cat := ""
	if e.Category != nil {
		cat = strings.ToLower(strings.TrimSpace(*e.Category))
	}
	switch {
	case cat == "pembayaran" || strings.Contains(cat, "payment"):
		return 3
	case strings.Contains(cat, "rekening"):
		return 2
	default:
		return 1
	}
}

// tryPaymentFAQAnswer returns KB payment/rekening info without calling the LLM.
func tryPaymentFAQAnswer(query string, kb []KBEntry) (string, bool) {
	if !IsPaymentQuestion(query) {
		return "", false
	}
	var best KBEntry
	var bestScore float64
	var fallback KBEntry
	var fallbackPri int
	var hasFallback bool
	whereToPay := asksWhereToPay(query)
	for _, e := range kb {
		if !e.IsActive {
			continue
		}
		cat := ""
		if e.Category != nil {
			cat = *e.Category
		}
		if !isPaymentKBEntry(e.Question, cat, e.Answer) {
			continue
		}
		pri := paymentKBFallbackPriority(e)
		if !hasFallback || pri > fallbackPri {
			fallback = e
			fallbackPri = pri
			hasFallback = true
		}
		score := paymentKBEntryScore(query, e)
		if whereToPay && accountNumberRe.MatchString(e.Answer) {
			score += 0.5
		}
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	pick := best
	if bestScore <= 0 && hasFallback {
		pick = fallback
	}
	if bestScore > 0 || hasFallback {
		if ans := strings.TrimSpace(pick.Answer); ans != "" {
			// An empty localization (Indonesian-only KB text for an English buyer) is "no answer":
			// never return ("", true); fall through to the direct FAQ match / the next routing step.
			if localized := strings.TrimSpace(LocalizeKBAnswerForBuyer(ans, query)); localized != "" {
				return localized, true
			}
		}
	}
	if direct, ok := tryFAQDirectAnswer(query, kb); ok {
		return direct, true
	}
	return "", false
}
