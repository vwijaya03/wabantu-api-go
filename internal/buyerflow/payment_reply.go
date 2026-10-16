package buyerflow

import "strings"

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
		ans := strings.TrimSpace(pick.Answer)
		if ans != "" {
			return LocalizeKBAnswerForBuyer(ans, query), true
		}
	}
	if direct, ok := tryFAQDirectAnswer(query, kb); ok {
		return direct, true
	}
	return "", false
}
