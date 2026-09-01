package ai

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestIsAnthropicRetryable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{context.DeadlineExceeded, true},
		{fmt.Errorf("anthropic API error: context deadline exceeded"), true},
		{fmt.Errorf(`dial tcp: lookup api.anthropic.com: no such host`), true},
		{fmt.Errorf("429 Too Many Requests"), true},
		{fmt.Errorf("500 internal server error"), true},
		{fmt.Errorf("503 service unavailable"), true},
		{fmt.Errorf("401 authentication_error"), false},
		{fmt.Errorf("invalid request: bad prompt"), false},
		{nil, false},
	}
	for _, tc := range cases {
		got := IsAnthropicRetryable(tc.err)
		if got != tc.want {
			t.Fatalf("IsAnthropicRetryable(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestShouldAckAnthropicFailure(t *testing.T) {
	if !ShouldAckAnthropicFailure(errors.New("401 invalid api key")) {
		t.Fatal("permanent should ack")
	}
	if ShouldAckAnthropicFailure(fmt.Errorf("context deadline exceeded")) {
		t.Fatal("retryable should not ack")
	}
}
