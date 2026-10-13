package ai

import (
	"context"
	"fmt"
	"strings"

	"encore.app/wabantu/aivision"
	"encore.app/wabantu/shared/llm"
	"github.com/anthropics/anthropic-sdk-go"
)

// AnthropicProvider adapts the legacy AnthropicClient (unchanged) to llm.Provider.
type AnthropicProvider struct {
	Client *AnthropicClient
	APIKey string // for vision (legacy aivision path)
}

var _ llm.Provider = (*AnthropicProvider)(nil)

func NewAnthropicProvider(apiKey string, cfg AnthropicConfig) *AnthropicProvider {
	return &AnthropicProvider{Client: NewAnthropicClient(apiKey, cfg), APIKey: apiKey}
}

func (p *AnthropicProvider) ID() string { return "anthropic" }

func toLLMUsage(model string, u CompletionUsage) llm.Usage {
	return llm.Usage{Provider: "anthropic", Model: model, InputTokens: int64(u.InputTokens), OutputTokens: int64(u.OutputTokens),
		CacheReadInputTokens: int64(u.CacheReadInputTokens), CacheCreationInputTokens: int64(u.CacheCreationInputTokens)}
}

// Complete delegates to AnthropicClient.CompleteText (same model resolution + fallback list).
func (p *AnthropicProvider) Complete(ctx context.Context, model, system, user string, maxTok int64) (string, llm.Usage, error) {
	if strings.TrimSpace(model) == "" {
		model = p.Client.model
	}
	text, u, err := p.Client.CompleteText(ctx, model, system, user, maxTok)
	return text, toLLMUsage(ResolveAnthropicModel(model), u), err
}

// Chat sends a generic multi-turn conversation (no tools; tool-use stays in anthropic_tools.go until BE-04).
func (p *AnthropicProvider) Chat(ctx context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	model := r.Model
	if strings.TrimSpace(model) == "" {
		model = p.Client.model
	}
	model = ResolveAnthropicModel(model)
	maxTok := r.MaxTokens
	if maxTok <= 0 {
		maxTok = p.Client.maxTok
	}
	var msgs []anthropic.MessageParam
	for _, m := range r.Messages {
		if m.Role == "assistant" {
			msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Text)))
		} else {
			msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Text)))
		}
	}
	params := anthropic.MessageNewParams{Model: anthropic.Model(model), MaxTokens: maxTok, Messages: msgs}
	if r.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: r.System}}
	}
	resp, err := p.Client.client.Messages.New(ctx, params)
	if err != nil {
		return llm.ChatResponse{Usage: llm.Usage{Provider: "anthropic", Model: model}}, fmt.Errorf("anthropic API error: %w", err)
	}
	var parts []string
	for _, b := range resp.Content {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	u := toLLMUsage(model, usageFromResponse(resp))
	if text == "" {
		return llm.ChatResponse{Usage: u}, fmt.Errorf("AI response kosong")
	}
	return llm.ChatResponse{Text: text, Usage: u}, nil
}

// Vision delegates to the legacy aivision Anthropic path.
func (p *AnthropicProvider) Vision(ctx context.Context, model string, img []byte, mime, system, user string) (string, llm.Usage, error) {
	return aivision.AnthropicVision{APIKey: p.APIKey}.Vision(ctx, model, img, mime, system, user)
}
