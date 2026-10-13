package ai

import (
	"context"
	"strings"
	"sync"

	"encore.app/wabantu/shared/llm"
)

// Text-LLM registry for ai + business. Anthropic is always registered; other providers
// (Gemini, BE-04) are added with RegisterLLMProvider. With no DB store wired, the
// resolver yields code defaults (Anthropic, model "") = legacy behaviour.
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

// LLMRegistry builds a registry whose Anthropic provider uses apiKey/cfg (legacy client settings).
func LLMRegistry(apiKey string, cfg AnthropicConfig) *llm.Registry {
	llmMu.RLock()
	defer llmMu.RUnlock()
	reg := llm.NewRegistry(llmResolver)
	for _, p := range llmExtra {
		reg.Register(p)
	}
	reg.Register(NewAnthropicProvider(apiKey, cfg))
	return reg
}

// CompleteViaRegistry replaces direct NewAnthropicClient(...).CompleteText calls.
// defaultModel is used only when the resolved provider is Anthropic and config gives no model,
// so Claude model ids are never sent to another provider. Returns the model actually used.
func CompleteViaRegistry(ctx context.Context, apiKey string, cfg AnthropicConfig, tenantID string, fn llm.Function,
	defaultModel, system, user string, maxTok int64) (string, CompletionUsage, string, error) {
	p, model, err := LLMRegistry(apiKey, cfg).For(ctx, tenantID, fn)
	if err != nil {
		return "", CompletionUsage{}, defaultModel, err
	}
	if ap, ok := p.(*AnthropicProvider); ok {
		if strings.TrimSpace(model) == "" {
			model = defaultModel
		}
		// Call the legacy client directly so usage (incl. cache counters) and errors are byte-identical.
		text, u, err := ap.Client.CompleteText(ctx, model, system, user, maxTok)
		return text, u, model, err
	}
	text, u, err := p.Complete(ctx, model, system, user, maxTok)
	return text, CompletionUsage{
		InputTokens: int(u.InputTokens), OutputTokens: int(u.OutputTokens),
		CacheReadInputTokens: int(u.CacheReadInputTokens), CacheCreationInputTokens: int(u.CacheCreationInputTokens),
	}, u.Model, err
}

// ChatClientViaRegistry returns the Anthropic client for the auto-reply pipeline.
// Sales chat + tool-use is Anthropic-specific (anthropic_tools.go) until BE-04, so this
// always yields Anthropic; the registry lookup is kept so the switch point exists.
func ChatClientViaRegistry(apiKey string, cfg AnthropicConfig) *AnthropicClient {
	reg := LLMRegistry(apiKey, cfg)
	if p, ok := reg.Get("anthropic"); ok {
		return p.(*AnthropicProvider).Client
	}
	return NewAnthropicClient(apiKey, cfg)
}
