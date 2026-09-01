package ai

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

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
		client: anthropic.NewClient(
			option.WithAPIKey(apiKey),
			option.WithHTTPClient(&http.Client{Timeout: 20 * time.Second}),
		),
		model:  cfg.Model,
		maxTok: int64(cfg.MaxTokens),
	}
}

// CompletionUsage holds token counts from the Anthropic API.
type CompletionUsage struct {
	InputTokens              int
	OutputTokens             int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
}

const (
	anthropicMaxRetries = 3
	anthropicRetryBase  = 500 * time.Millisecond
)

func anthropicRetryDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	base := anthropicRetryBase * time.Duration(1<<attempt)
	jitter := time.Duration(rand.Int63n(int64(base/2) + 1))
	return base + jitter
}

func (c *AnthropicClient) GenerateReply(ctx context.Context, req SalesReplyRequest) (string, error) {
	text, _, err := c.GenerateReplyWithModel(ctx, c.model, req)
	return text, err
}

// GenerateReplyWithModel runs completion on the given model (Haiku / Sonnet hybrid routing).
func (c *AnthropicClient) GenerateReplyWithModel(ctx context.Context, model string, req SalesReplyRequest) (text string, usage CompletionUsage, err error) {
	if strings.TrimSpace(model) == "" {
		model = c.model
	}
	model = ResolveAnthropicModel(model)

	var lastErr error
	for i, tryModel := range FallbackModels(model) {
		text, usage, err = c.generateOnceWithRetry(ctx, tryModel, req, 0.3)
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

func (c *AnthropicClient) generateOnceWithRetry(ctx context.Context, model string, req SalesReplyRequest, temperature float64) (string, CompletionUsage, error) {
	var usage CompletionUsage
	var lastErr error
	for attempt := 0; attempt < anthropicMaxRetries; attempt++ {
		if attempt > 0 {
			delay := anthropicRetryDelay(attempt - 1)
			rlog.Warn("anthropic retrying after transient error", "model", model, "attempt", attempt+1, "delay", delay)
			select {
			case <-ctx.Done():
				return "", usage, ctx.Err()
			case <-time.After(delay):
			}
		}
		text, u, err := c.generateOnce(ctx, model, req, temperature)
		if err == nil {
			return text, u, nil
		}
		lastErr = err
		if !IsAnthropicRetryable(err) || attempt == anthropicMaxRetries-1 {
			return "", usage, err
		}
	}
	return "", usage, lastErr
}

func (c *AnthropicClient) generateOnce(ctx context.Context, model string, req SalesReplyRequest, temperature float64) (text string, usage CompletionUsage, err error) {
	systemBlocks := buildSalesSystemBlocks(req)
	messages := buildSalesMessages(req)

	rlog.Info("anthropic request",
		"model", model,
		"maxTokens", c.maxTok,
		"sysBlocks", len(systemBlocks),
		"businessLen", len(req.Business),
		"kbLen", len(req.KB),
		"histTurns", len(req.History),
		"userMsgLen", len(req.UserText),
	)

	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:       anthropic.Model(model),
		MaxTokens:   c.maxTok,
		Temperature: anthropic.Float(temperature),
		System:      systemBlocks,
		Messages:    messages,
	})
	if err != nil {
		rlog.Error("anthropic messages.create failed", "err", err, "model", model)
		return "", usage, err
	}
	usage = usageFromResponse(resp)

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

	if resp.StopReason == anthropic.StopReasonMaxTokens {
		rlog.Warn("anthropic completion truncated by max_tokens", "model", model, "outputTok", usage.OutputTokens)
	}

	text = finalizeAnthropicReply(text, resp.StopReason)

	rlog.Info("anthropic completion received",
		"model", model,
		"len", len(text),
		"inputTok", usage.InputTokens,
		"outputTok", usage.OutputTokens,
		"cacheReadTok", usage.CacheReadInputTokens,
		"cacheCreateTok", usage.CacheCreationInputTokens,
		"stopReason", resp.StopReason,
	)
	return text, usage, nil
}

// CompleteText runs a single user prompt with system instruction (summarize, etc.).
func (c *AnthropicClient) CompleteText(ctx context.Context, model, system, user string, maxTokens int64) (string, CompletionUsage, error) {
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
			return "", CompletionUsage{}, fmt.Errorf("anthropic API error: %w", err)
		}
		var parts []string
		for _, block := range resp.Content {
			if block.Type == "text" {
				parts = append(parts, block.Text)
			}
		}
		text := strings.TrimSpace(strings.Join(parts, "\n"))
		if text == "" {
			return "", CompletionUsage{}, fmt.Errorf("empty completion from model %s", tryModel)
		}
		usage := usageFromResponse(resp)
		if i > 0 {
			rlog.Info("anthropic CompleteText succeeded after fallback", "model", tryModel)
		}
		return text, usage, nil
	}
	return "", CompletionUsage{}, fmt.Errorf("anthropic summarize: %w", lastErr)
}
