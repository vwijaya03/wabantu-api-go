package llm

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
)

// IsProviderRetryable reports provider-neutral transient errors:
//   - typed *APIError with 429/5xx
//   - transport: io.ErrUnexpectedEOF, context.DeadlineExceeded, net.Error with Timeout()
//     (e.g. http.Client.Timeout) — regardless of whether the chain link has timeout_ms.
//     A cancelled context (context.Canceled) is never retryable; Registry.Run additionally
//     stops when the parent ctx is done, so a parent deadline never advances the chain.
//   - text: "429 Too Many Requests", Google status RESOURCE_EXHAUSTED, and UNAVAILABLE only
//     as a status/code (or HTTP 503) — not free-text "unavailable" (e.g. 404 "model unavailable").
func IsProviderRetryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Retryable()
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return retryableText(err.Error())
}

func retryableText(msg string) bool {
	low := strings.ToLower(msg)
	if strings.Contains(low, "429 too many requests") || strings.Contains(low, "too many requests") ||
		strings.Contains(low, "resource_exhausted") || strings.Contains(low, "503 service unavailable") {
		return true
	}
	// Google/gRPC status form only: "UNAVAILABLE" enum, "code = Unavailable", `"status": "UNAVAILABLE"`.
	if strings.Contains(msg, "UNAVAILABLE") || strings.Contains(msg, "code = Unavailable") {
		return true
	}
	return false
}
