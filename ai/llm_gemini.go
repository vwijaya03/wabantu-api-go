package ai

import (
	"os"
	"strings"
	"sync"

	"encore.app/wabantu/aivision"
	"encore.app/wabantu/shared/llm"
	"encore.dev/rlog"
)

// geminiKeyEnv is read from the process env (Cloud Run: Secret Manager → env var).
// Deliberately NOT an Encore `secrets` field: a declared-but-unset Encore secret can fail
// local runs/deploy validation, and the key must stay optional (BE-07 decides the final wiring).
const geminiKeyEnv = "GeminiAPIKey"

var geminiOnce sync.Once

// lookupGeminiKey is swappable in tests.
var lookupGeminiKey = func() string { return strings.TrimSpace(os.Getenv(geminiKeyEnv)) }

// ensureGeminiRegistered lazily registers Gemini (text + vision) on first registry use,
// only when a key is present. Without a key nothing is registered and chains fall back
// to Anthropic; that is logged once here instead of per call.
func ensureGeminiRegistered() {
	geminiOnce.Do(func() {
		key := lookupGeminiKey()
		llm.SetProviderAvailable("anthropic", strings.TrimSpace(secrets.AnthropicAPIKey) != "")
		llm.SetProviderAvailable("gemini", key != "")
		if key == "" {
			rlog.Info("llm: GeminiAPIKey not set; gemini provider not registered, chains fall back to anthropic")
			return
		}
		g := llm.NewGeminiProvider(key)
		RegisterLLMProvider(g)
		aivision.RegisterVisionProvider(g)
		rlog.Info("llm: gemini provider registered")
	})
}
