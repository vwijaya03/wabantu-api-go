package ai

import "strings"

// tryPaymentFAQAnswer returns KB payment/rekening info without calling the LLM.
func tryPaymentFAQAnswer(query string, kb []dbKBEntry) (string, bool) {
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
