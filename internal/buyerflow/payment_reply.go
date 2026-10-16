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
	if strings.Contains(q, "rekening") || strings.Contains(q, "bank") ||
		strings.Contains(q, "transfer") || strings.Contains(q, "pay") || strings.Contains(q, "payment") {
		if strings.Contains(text, "bca") || strings.Contains(text, "rekening") ||
			strings.Contains(text, "bank transfer") || strings.Contains(text, "transfer bank") {
			score += 0.4
		}
	}
	return score
}

// tryPaymentFAQAnswer returns KB payment/rekening info without calling the LLM.
func tryPaymentFAQAnswer(query string, kb []KBEntry) (string, bool) {
	if !IsPaymentQuestion(query) {
		return "", false
	}
	var best KBEntry
	var bestScore float64
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
		score := paymentKBEntryScore(query, e)
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	if bestScore > 0 {
		ans := strings.TrimSpace(best.Answer)
		if ans != "" {
			return LocalizeKBAnswerForBuyer(ans, query), true
		}
	}
	if direct, ok := tryFAQDirectAnswer(query, kb); ok {
		return direct, true
	}
	return "", false
}
