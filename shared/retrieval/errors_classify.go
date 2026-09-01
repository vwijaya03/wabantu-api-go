package retrieval

import (
	"context"
	"errors"
	"strings"
)

// FallbackReason classifies why vector retrieval fell back to lexical.
type FallbackReason string

const (
	FallbackReasonNone            FallbackReason = ""
	FallbackReasonDisabled        FallbackReason = "disabled"
	FallbackReasonNotConfigured   FallbackReason = "not_configured"
	FallbackReasonCircuitOpen     FallbackReason = "circuit_open"
	FallbackReasonClientTimeout   FallbackReason = "client_timeout"
	FallbackReasonEmbedError      FallbackReason = "embed_error"
	FallbackReasonQueryError      FallbackReason = "query_error"
	FallbackReasonLexicalOnlyMode FallbackReason = "lexical_only_mode"
)

// IsClientTimeout reports caller-side deadline/cancel (should not trip circuit breaker).
func IsClientTimeout(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// ShouldTripBreaker returns false for client timeouts and missing configuration.
func ShouldTripBreaker(err error) bool {
	if err == nil {
		return false
	}
	if IsClientTimeout(err) || errors.Is(err, ErrServiceNotConfigured) {
		return false
	}
	return true
}

// ClassifyVectorError maps provider errors to fallback reason labels.
func ClassifyVectorError(err error) FallbackReason {
	if err == nil {
		return FallbackReasonNone
	}
	if IsClientTimeout(err) {
		return FallbackReasonClientTimeout
	}
	if errors.Is(err, ErrServiceNotConfigured) {
		return FallbackReasonNotConfigured
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "embed") || strings.Contains(msg, "openai") {
		return FallbackReasonEmbedError
	}
	return FallbackReasonQueryError
}
