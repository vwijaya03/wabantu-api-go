package llm

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// TransportError marks a failure before any HTTP response was received (dial, write,
// connection closed / stale keep-alive "EOF", client timeout). Always retryable unless
// the cause is context.Canceled.
type TransportError struct {
	Provider string
	Err      error
}

func (e *TransportError) Error() string { return e.Provider + ": transport: " + e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }

// Google/gRPC status form only (JSON "status": "UNAVAILABLE", "status: UNAVAILABLE", gRPC "code = Unavailable").
var reUnavailableStatus = regexp.MustCompile(`"?status"?\s*[:=]\s*"?UNAVAILABLE\b|code\s*=\s*Unavailable\b`)

// IsProviderRetryable reports provider-neutral transient errors:
//   - typed *APIError with 429/5xx
//   - transport: io.ErrUnexpectedEOF, context.DeadlineExceeded, net.Error with Timeout()
//     (e.g. http.Client.Timeout) — regardless of whether the chain link has timeout_ms.
//     A cancelled context (context.Canceled) is never retryable; Registry.Run additionally
//     stops when the parent ctx is done, so a parent deadline never advances the chain.
//   - *TransportError (no response received), or *url.Error wrapping io.EOF
//   - text: "429 Too Many Requests", RESOURCE_EXHAUSTED, "503 Service Unavailable", and
//     UNAVAILABLE only in status/code form — not free text (e.g. 404 "model unavailable").
//
// A typed *APIError always wins: its HTTP status decides (429/5xx), whatever the body/read error.
func IsProviderRetryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Retryable()
	}
	var te *TransportError
	if errors.As(err, &te) {
		return true
	}
	// Other SDKs: http.Client.Do returns *url.Error with no response; a bare EOF there means
	// the connection closed before a response (stale keep-alive). Plain io.EOF elsewhere is not retryable.
	var ue *url.Error
	if errors.As(err, &ue) && errors.Is(ue.Err, io.EOF) {
		return true
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
	return reUnavailableStatus.MatchString(msg)
}
