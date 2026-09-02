package retrieval

import (
	"context"
	"errors"
	"net"
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

// Provider names external retrieval dependencies.
type Provider string

const (
	ProviderOpenAI   Provider = "openai"
	ProviderPinecone Provider = "pinecone"
)

// ErrorCategory classifies provider failures for breaker and observability.
type ErrorCategory string

const (
	CategoryCallerCanceled  ErrorCategory = "caller_canceled"
	CategoryBudgetExceeded  ErrorCategory = "budget_exceeded"
	CategoryProviderTimeout ErrorCategory = "provider_timeout"
	CategoryProviderRate    ErrorCategory = "provider_429"
	CategoryProvider5xx     ErrorCategory = "provider_5xx"
	CategoryNetwork         ErrorCategory = "network_error"
	CategoryInvalidRequest  ErrorCategory = "invalid_request"
	CategoryConfiguration   ErrorCategory = "configuration_error"
)

// ErrCircuitOpen is returned when the per-tenant circuit breaker is open.
var ErrCircuitOpen = errors.New("retrieval circuit breaker open")

// RetrievalError is a structured provider failure with breaker semantics.
type RetrievalError struct {
	Category     ErrorCategory
	Provider     Provider
	Retryable    bool
	TripsBreaker bool
}

// IsClientTimeout reports caller-side deadline/cancel on the immediate context.
func IsClientTimeout(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func parentContextAlive(parentCtx context.Context) bool {
	if parentCtx == nil {
		return true
	}
	select {
	case <-parentCtx.Done():
		return false
	default:
		return true
	}
}

// ClassifyProviderError maps provider errors to categories and breaker eligibility.
// When err is context.DeadlineExceeded, parentCtx determines caller_canceled vs budget_exceeded.
func ClassifyProviderError(parentCtx context.Context, err error, p Provider) RetrievalError {
	if err == nil {
		return RetrievalError{}
	}
	if errors.Is(err, ErrCircuitOpen) {
		return RetrievalError{
			Category:     CategoryConfiguration,
			Provider:     p,
			Retryable:    false,
			TripsBreaker: false,
		}
	}
	if errors.Is(err, ErrServiceNotConfigured) {
		return RetrievalError{
			Category:     CategoryConfiguration,
			Provider:     p,
			Retryable:    true,
			TripsBreaker: false,
		}
	}
	if errors.Is(err, context.Canceled) {
		return RetrievalError{
			Category:     CategoryCallerCanceled,
			Provider:     p,
			Retryable:    false,
			TripsBreaker: false,
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		if parentContextAlive(parentCtx) {
			return RetrievalError{
				Category:     CategoryBudgetExceeded,
				Provider:     p,
				Retryable:    true,
				TripsBreaker: true,
			}
		}
		return RetrievalError{
			Category:     CategoryCallerCanceled,
			Provider:     p,
			Retryable:    false,
			TripsBreaker: false,
		}
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "429") || strings.Contains(msg, "rate limit") {
		return RetrievalError{
			Category:     CategoryProviderRate,
			Provider:     p,
			Retryable:    true,
			TripsBreaker: true,
		}
	}
	for _, code := range []string{"500", "502", "503", "504"} {
		if strings.Contains(msg, code) {
			return RetrievalError{
				Category:     CategoryProvider5xx,
				Provider:     p,
				Retryable:    true,
				TripsBreaker: true,
			}
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return RetrievalError{
			Category:     CategoryProviderTimeout,
			Provider:     p,
			Retryable:    true,
			TripsBreaker: true,
		}
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") {
		return RetrievalError{
			Category:     CategoryProviderTimeout,
			Provider:     p,
			Retryable:    true,
			TripsBreaker: true,
		}
	}
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "tls") ||
		strings.Contains(msg, "eof") {
		return RetrievalError{
			Category:     CategoryNetwork,
			Provider:     p,
			Retryable:    true,
			TripsBreaker: true,
		}
	}
	if strings.Contains(msg, "400") || strings.Contains(msg, "invalid") {
		return RetrievalError{
			Category:     CategoryInvalidRequest,
			Provider:     p,
			Retryable:    false,
			TripsBreaker: false,
		}
	}
	if p == ProviderOpenAI && (strings.Contains(msg, "embed") || strings.Contains(msg, "openai")) {
		return RetrievalError{
			Category:     CategoryProvider5xx,
			Provider:     p,
			Retryable:    true,
			TripsBreaker: true,
		}
	}
	return RetrievalError{
		Category:     CategoryNetwork,
		Provider:     p,
		Retryable:    true,
		TripsBreaker: true,
	}
}

// ShouldTripBreaker reports whether the error should count toward opening the circuit.
func ShouldTripBreaker(parentCtx context.Context, err error, provider Provider) bool {
	return ClassifyProviderError(parentCtx, err, provider).TripsBreaker
}

// FallbackReasonFromCategory maps structured categories to legacy fallback labels.
func FallbackReasonFromCategory(re RetrievalError) FallbackReason {
	switch re.Category {
	case CategoryCallerCanceled, CategoryBudgetExceeded:
		return FallbackReasonClientTimeout
	case CategoryConfiguration:
		return FallbackReasonNotConfigured
	case CategoryInvalidRequest:
		if re.Provider == ProviderPinecone {
			return FallbackReasonQueryError
		}
		return FallbackReasonEmbedError
	default:
		if re.Provider == ProviderPinecone {
			return FallbackReasonQueryError
		}
		return FallbackReasonEmbedError
	}
}

// ClassifyVectorError maps provider errors to fallback reason labels (legacy API).
func ClassifyVectorError(err error) FallbackReason {
	return FallbackReasonFromCategory(ClassifyProviderError(context.Background(), err, ProviderOpenAI))
}

// ClassifyVectorErrorWithContext classifies using parent context for deadline semantics.
func ClassifyVectorErrorWithContext(parentCtx context.Context, err error, provider Provider) FallbackReason {
	return FallbackReasonFromCategory(ClassifyProviderError(parentCtx, err, provider))
}
