package admin

import (
	"strings"
	"testing"

	"encore.dev/beta/errs"

	"encore.app/wabantu/shared/retrieval"
)

func TestRequireReindexConfirmation(t *testing.T) {
	if err := requireReindexConfirmation(retrieval.EmbeddingModel, retrieval.EmbeddingModel, false); err != nil {
		t.Fatalf("same model needs no confirmation: %v", err)
	}
	if err := requireReindexConfirmation(retrieval.EmbeddingModel, retrieval.GeminiEmbeddingModel, true); err != nil {
		t.Fatalf("confirmed change must pass: %v", err)
	}
	err := requireReindexConfirmation(retrieval.EmbeddingModel, retrieval.GeminiEmbeddingModel, false)
	if err == nil {
		t.Fatal("unconfirmed model change must be rejected")
	}
	if e, ok := err.(*errs.Error); !ok || e.Code != errs.FailedPrecondition {
		t.Fatalf("want failed_precondition, got %#v", err)
	}
	if !strings.Contains(err.Error(), "confirmReindex") {
		t.Fatalf("message should name the flag: %v", err)
	}
}

func TestRequireEmbeddingCredentialsGemini(t *testing.T) {
	t.Setenv("GeminiAPIKey", "")
	if err := requireEmbeddingCredentials(retrieval.GeminiEmbeddingModel); err == nil {
		t.Fatal("gemini without key must be rejected")
	}
	if err := requireEmbeddingCredentials(retrieval.EmbeddingModel); err != nil {
		t.Fatalf("openai needs no gemini key: %v", err)
	}
	t.Setenv("GeminiAPIKey", "present")
	if err := requireEmbeddingCredentials(retrieval.GeminiEmbeddingModel); err != nil {
		t.Fatalf("gemini with key: %v", err)
	}
}
