package ai

import (
	"errors"
	"strings"
	"testing"
)

func TestSanitizeRetrievalErrorStripsURLAndTruncates(t *testing.T) {
	t.Parallel()
	raw := "Post https://api.openai.com/v1/embeddings?api_key=secret: context deadline exceeded " + strings.Repeat("x", 300)
	safe := SanitizeRetrievalError(errors.New(raw))
	if strings.Contains(safe, "https://") {
		t.Fatal("URL should be stripped")
	}
	if strings.Contains(safe, "api_key") {
		t.Fatal("query params should be stripped")
	}
	if len(safe) > 256 {
		t.Fatalf("safe error too long: %d", len(safe))
	}
}

func TestSanitizeRetrievalErrorNil(t *testing.T) {
	if SanitizeRetrievalError(nil) != "" {
		t.Fatal("nil error should return empty string")
	}
}
