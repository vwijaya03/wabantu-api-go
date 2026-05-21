package ai

import (
	"context"
	"fmt"
	"strings"

	"encore.dev/rlog"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func isModelNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not_found") && strings.Contains(s, "model")
}

type AnthropicConfig struct {
	Model     string
	MaxTokens int
}

func DefaultAnthropicConfig() AnthropicConfig {
	return AnthropicConfig{
		Model:     DefaultSonnetAPIID(),
		MaxTokens: 512,
	}
}

type AnthropicClient struct {
	client anthropic.Client
	model  string
	maxTok int64
}

func NewAnthropicClient(apiKey string, cfg AnthropicConfig) *AnthropicClient {
	if cfg.Model == "" {
		cfg.Model = DefaultSonnetAPIID()
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 512
	}
	return &AnthropicClient{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:  cfg.Model,
		maxTok: int64(cfg.MaxTokens),
	}
}

// CompletionUsage holds token counts from the Anthropic API.
type CompletionUsage struct {
	InputTokens  int
	OutputTokens int
}

func (c *AnthropicClient) GenerateReply(ctx context.Context, system, business, kb, history, userMessage string) (string, error) {
	text, _, err := c.GenerateReplyWithModel(ctx, c.model, system, business, kb, history, userMessage)
	return text, err
}

// GenerateReplyWithModel runs completion on the given model (Haiku / Sonnet hybrid routing).
func (c *AnthropicClient) GenerateReplyWithModel(ctx context.Context, model, system, business, kb, history, userMessage string) (text string, usage CompletionUsage, err error) {
	if strings.TrimSpace(model) == "" {
		model = c.model
	}
	model = ResolveAnthropicModel(model)

	var lastErr error
	for i, tryModel := range FallbackModels(model) {
		text, usage, err = c.generateOnce(ctx, tryModel, system, business, kb, history, userMessage)
		if err == nil {
			if i > 0 {
				rlog.Info("anthropic completion succeeded after fallback", "model", tryModel, "attempt", i+1)
			}
			return text, usage, nil
		}
		lastErr = err
		if !isModelNotFoundErr(err) {
			return "", usage, err
		}
		rlog.Warn("anthropic model not found, trying next", "model", tryModel, "err", err)
	}
	return "", usage, fmt.Errorf("anthropic API error: %w", lastErr)
}

func (c *AnthropicClient) generateOnce(ctx context.Context, model, system, business, kb, history, userMessage string) (text string, usage CompletionUsage, err error) {
	prompt := strings.Join([]string{
		"Konteks bisnis:",
		business,
		"",
		kb,
		"",
		history,
		"",
		fmt.Sprintf("Pesan pelanggan terbaru: %s", userMessage),
		"Tugas: berikan satu balasan WhatsApp yang aman dan membantu.",
	}, "\n")

	rlog.Info("anthropic request",
		"model", model,
		"maxTokens", c.maxTok,
		"sysLen", len(system),
		"businessLen", len(business),
		"kbLen", len(kb),
		"histLen", len(history),
		"userMsgLen", len(userMessage),
	)

	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:       anthropic.Model(model),
		MaxTokens:   c.maxTok,
		Temperature: anthropic.Float(0.3),
		System: []anthropic.TextBlockParam{
			{Text: system},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		rlog.Error("anthropic messages.create failed", "err", err, "model", model)
		return "", usage, err
	}
	usage = CompletionUsage{
		InputTokens:  int(resp.Usage.InputTokens),
		OutputTokens: int(resp.Usage.OutputTokens),
	}

	var parts []string
	for _, block := range resp.Content {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	text = strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		rlog.Warn("anthropic returned empty completion", "model", model)
		return "", usage, fmt.Errorf("AI response kosong")
	}

	rlog.Info("anthropic completion received", "model", model, "len", len(text), "inputTok", usage.InputTokens, "outputTok", usage.OutputTokens)
	if len(text) > 1200 {
		text = text[:1200]
	}
	return text, usage, nil
}

// CompleteText runs a single user prompt with system instruction (summarize, etc.).
func (c *AnthropicClient) CompleteText(ctx context.Context, model, system, user string, maxTokens int64) (string, error) {
	if maxTokens <= 0 {
		maxTokens = c.maxTok
	}
	model = ResolveAnthropicModel(model)
	var lastErr error
	for i, tryModel := range FallbackModels(model) {
		resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(tryModel),
			MaxTokens: maxTokens,
			System: []anthropic.TextBlockParam{
				{Text: system},
			},
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
			},
		})
		if err != nil {
			lastErr = err
			if isModelNotFoundErr(err) {
				rlog.Warn("anthropic CompleteText model not found", "model", tryModel)
				continue
			}
			return "", fmt.Errorf("anthropic API error: %w", err)
		}
		var parts []string
		for _, block := range resp.Content {
			if block.Type == "text" {
				parts = append(parts, block.Text)
			}
		}
		text := strings.TrimSpace(strings.Join(parts, "\n"))
		if text == "" {
			return "", fmt.Errorf("empty completion from model %s", tryModel)
		}
		if i > 0 {
			rlog.Info("anthropic CompleteText succeeded after fallback", "model", tryModel)
		}
		return text, nil
	}
	return "", fmt.Errorf("anthropic summarize: %w", lastErr)
}
