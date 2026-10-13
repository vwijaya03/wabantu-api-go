package llm

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsProviderRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"resource exhausted", errors.New(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`), true},
		{"unavailable", errors.New(`{"error":{"code":503,"status":"UNAVAILABLE"}}`), true},
		{"typed 429", &APIError{Status: 429}, true},
		{"typed 503 wrapped", fmt.Errorf("x: %w", &APIError{Status: 503}), true},
		{"typed 400", &APIError{Status: 400, Body: "INVALID_ARGUMENT"}, false},
		{"permission", errors.New("PERMISSION_DENIED"), false},
	}
	for _, c := range cases {
		if got := IsProviderRetryable(c.err); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
