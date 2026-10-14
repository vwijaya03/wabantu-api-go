package ai

import (
	"sync"
	"testing"
)

// Requires encore test (ai package init needs the Encore runtime).
func TestEnsureGeminiRegisteredOnlyWithKey(t *testing.T) {
	old := lookupGeminiKey
	t.Cleanup(func() {
		lookupGeminiKey = old
		geminiOnce = sync.Once{}
		llmMu.Lock()
		delete(llmExtra, "gemini")
		llmMu.Unlock()
	})

	geminiOnce = sync.Once{}
	lookupGeminiKey = func() string { return "" }
	ensureGeminiRegistered()
	if _, ok := LLMRegistry("", DefaultAnthropicConfig()).Get("gemini"); ok {
		t.Fatal("gemini must not be registered without key")
	}

	geminiOnce = sync.Once{}
	lookupGeminiKey = func() string { return "fake" }
	ensureGeminiRegistered()
	if _, ok := LLMRegistry("", DefaultAnthropicConfig()).Get("gemini"); !ok {
		t.Fatal("gemini should be registered when key present")
	}
}
