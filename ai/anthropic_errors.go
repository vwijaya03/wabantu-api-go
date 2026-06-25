package ai

import (
	"context"
	"errors"
	"net"
	"strings"
)

// IsAnthropicRetryable reports transient Anthropic/network failures worth pubsub retry.
func IsAnthropicRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	s := strings.ToLower(err.Error())
	for _, p := range []string{
		"timeout", "deadline exceeded", "connection reset", "connection refused",
		"temporary failure", "no such host", "i/o timeout", "eof",
		"429", "too many requests", "overloaded", "529",
		"502", "503", "504",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// IsAnthropicPermanent reports errors that should not be retried indefinitely.
func IsAnthropicPermanent(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, p := range []string{
		"401", "authentication", "invalid api key", "permission denied", "invalid_request",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// ShouldAckAnthropicFailure — best-effort background jobs may ack after logging.
func ShouldAckAnthropicFailure(err error) bool {
	if err == nil {
		return false
	}
	if IsAnthropicPermanent(err) {
		return true
	}
	return !IsAnthropicRetryable(err)
}
