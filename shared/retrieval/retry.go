package retrieval

import (
	"errors"
	"net"
	"strings"
)

const MaxIndexAttempts = 8

// IsRetryableError classifies provider/network errors for outbox retry.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"429", "408", "500", "502", "503", "504", "timeout", "temporarily", "rate limit"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// ShouldDLQ returns true when attempts exceeded max retries.
func ShouldDLQ(attempts int) bool {
	return attempts >= MaxIndexAttempts
}
