package ai

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"encore.app/wabantu/aivision"
	"encore.app/wabantu/shared/llm"
	"encore.dev/rlog"
)

// Text-LLM registry for ai + business. Anthropic is always registered; other providers
// (Gemini, BE-04) are added with RegisterLLMProvider. The resolver is DB-backed once
// WireLLMConfig runs at service start; with empty tables it yields code defaults (Anthropic).
var (
	llmMu       sync.RWMutex
	llmExtra    = map[string]llm.Provider{}
	llmResolver = llm.NewResolver(nil, 0)
)

func RegisterLLMProvider(p llm.Provider) {
	llmMu.Lock()
	llmExtra[p.ID()] = p
	llmMu.Unlock()
}

func SetLLMResolver(r *llm.Resolver) {
	llmMu.Lock()
	llmResolver = r
	llmMu.Unlock()
}

// WireLLMConfig points both the text registry and the aivision registry at one DB-backed
// resolver (TTL 30s, no Pub/Sub invalidation yet) and routes llm diagnostics to rlog.
func WireLLMConfig(db *sql.DB) {
	llm.Logf = func(format string, args ...any) { rlog.Warn("llm: " + fmt.Sprintf(format, args...)) }
	llm.Debugf = func(format string, args ...any) { rlog.Debug("llm: " + fmt.Sprintf(format, args...)) }
	if db == nil {
		return
	}
	r := llm.NewResolver(llm.NewSQLStore(db), llm.DefaultTTL)
	SetLLMResolver(r)
	aivision.SetVisionResolver(r)
	ensureGeminiRegistered() // vision callers may hit aivision before any text registry use
}

// LLMRegistry builds a registry whose Anthropic provider uses apiKey/cfg (legacy client settings).
func LLMRegistry(apiKey string, cfg AnthropicConfig) *llm.Registry {
	ensureGeminiRegistered()
	llmMu.RLock()
	defer llmMu.RUnlock()
	reg := llm.NewRegistry(llmResolver)
	for _, p := range llmExtra {
		reg.Register(p)
	}
	reg.Register(NewAnthropicProvider(apiKey, cfg))
	return reg
}

// CompleteViaRegistry replaces direct NewAnthropicClient(...).CompleteText calls and runs the
// configured chain with fallback on retryable errors. defaultModel is used only for Anthropic
// links without a model, so Claude ids are never sent to another provider. Returns the model used.
func CompleteViaRegistry(ctx context.Context, apiKey string, cfg AnthropicConfig, tenantID string, fn llm.Function,
	defaultModel, system, user string, maxTok int64) (string, CompletionUsage, string, error) {
	var (
		text      string
		usage     CompletionUsage
		usedModel = defaultModel
	)
	_, err := LLMRegistry(apiKey, cfg).Run(ctx, tenantID, fn, func(ctx context.Context, p llm.Provider, model string) error {
		if ap, ok := p.(*AnthropicProvider); ok {
			if strings.TrimSpace(model) == "" {
				model = defaultModel
			}
			// Legacy client directly: usage (incl. cache counters) and errors byte-identical.
			t, u, err := ap.Client.CompleteText(ctx, model, system, user, maxTok)
			text, usage, usedModel = t, u, model
			return err
		}
		t, u, err := p.Complete(ctx, model, system, user, maxTok)
		text, usedModel = t, u.Model
		usage = CompletionUsage{
			InputTokens: int(u.InputTokens), OutputTokens: int(u.OutputTokens),
			CacheReadInputTokens: int(u.CacheReadInputTokens), CacheCreationInputTokens: int(u.CacheCreationInputTokens),
		}
		return err
	})
	if err != nil {
		return "", usage, usedModel, err
	}
	return text, usage, usedModel, nil
}

// ChatClientViaRegistry returns the Anthropic client for the auto-reply pipeline (startup).
func ChatClientViaRegistry(apiKey string, cfg AnthropicConfig) *AnthropicClient {
	if p, ok := LLMRegistry(apiKey, cfg).Get("anthropic"); ok {
		return p.(*AnthropicProvider).Client
	}
	return NewAnthropicClient(apiKey, cfg)
}

// chatClientFor resolves the tenant's 'chat' chain. Sales chat + tool-use is Anthropic-only
// until BE-04, so the Anthropic client is always returned; a non-Anthropic first link is logged.
func (s *AutoReplyService) chatClientFor(ctx context.Context, tenantID string) *AnthropicClient {
	cands := LLMRegistry(secrets.AnthropicAPIKey, AnthropicConfig{Model: defaultAnthropicModel, MaxTokens: defaultAnthropicMaxToks}).
		Candidates(ctx, tenantID, llm.FnChat)
	if len(cands) > 0 && cands[0].Provider.ID() != "anthropic" {
		rlog.Warn("llm: chat chain prefers non-anthropic provider but tool-use chat is anthropic-only (BE-04); using anthropic",
			"tenantId", tenantID, "provider", cands[0].Provider.ID())
	}
	return s.anthropic
}

// paymentOCRModelForUsage records the real model; keeps the legacy label when unknown.
func paymentOCRModelForUsage(u aivision.Usage) string {
	if strings.TrimSpace(u.Model) != "" {
		return u.Model
	}
	return "claude-haiku-4-5"
}
