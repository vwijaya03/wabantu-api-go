package llm

import (
	"errors"
	"strings"
)

// gRPC/Google status names that mean "try again later" (Gemini: 429 → RESOURCE_EXHAUSTED, 503 → UNAVAILABLE).
var retryableStatuses = []string{"resource_exhausted", "unavailable"}

// IsProviderRetryable reports provider-neutral transient errors not covered by HTTP-code
// heuristics: a typed *APIError with 429/5xx, or a Google status string.
func IsProviderRetryable(err error) bool {
	if err == nil {
		return false
	}
	var ae *APIError
	if errors.As(err, &ae) && ae.Retryable() {
		return true
	}
	s := strings.ToLower(err.Error())
	for _, p := range retryableStatuses {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
