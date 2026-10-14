package ai

import (
	"errors"
	"testing"

	"encore.app/wabantu/shared/llm"
)

func TestIsAnthropicRetryableGeminiStatuses(t *testing.T) {
	for _, e := range []error{
		errors.New("gemini: RESOURCE_EXHAUSTED quota"),
		errors.New("status: UNAVAILABLE"),
		&llm.APIError{Status: 429, Body: `{"status":"RESOURCE_EXHAUSTED"}`},
	} {
		if !IsAnthropicRetryable(e) {
			t.Errorf("want retryable: %v", e)
		}
	}
	if IsAnthropicRetryable(errors.New("INVALID_ARGUMENT")) {
		t.Error("INVALID_ARGUMENT must not be retryable")
	}
}
