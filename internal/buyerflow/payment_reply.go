package buyerflow

import "strings"

func isPaymentKBEntry(question, category, answer string) bool {
	question = strings.ToLower(question)
	category = strings.ToLower(category)
	answer = strings.ToLower(answer)
	tags := []string{"payment", "rekening", "bank", "transfer", "pembayaran"}
	for _, t := range tags {
		if strings.Contains(question, t) || strings.Contains(category, t) || strings.Contains(answer, t) {
			return true
		}
	}
	return strings.Contains(question, "rekening") || strings.Contains(question, "transfer")
}

// tryPaymentFAQAnswer returns KB payment/rekening info without calling the LLM.
func tryPaymentFAQAnswer(query string, kb []KBEntry) (string, bool) {
	if !IsPaymentQuestion(query) {
		return "", false
	}
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
		ans := strings.TrimSpace(e.Answer)
		if ans != "" {
			return ans, true
		}
	}
	if direct, ok := tryFAQDirectAnswer(query, kb); ok {
		return direct, true
	}
	return "", false
}
