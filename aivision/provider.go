package aivision

import (
	"context"

	"encore.app/wabantu/shared/llm"
)

// AnthropicVision exposes the legacy visionExtract path as an llm.Provider (Vision only).
// Behaviour is identical to visionExtract: Haiku default, temperature 0.1, 4096 max tokens,
// markdown fences stripped.
type AnthropicVision struct{ APIKey string }

var _ llm.Provider = AnthropicVision{}

func (AnthropicVision) ID() string { return "anthropic" }

func (AnthropicVision) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, llm.ErrNotSupported
}

func (AnthropicVision) Complete(context.Context, string, string, string, int64) (string, llm.Usage, error) {
	return "", llm.Usage{}, llm.ErrNotSupported
}

func (a AnthropicVision) Vision(ctx context.Context, model string, img []byte, mime, system, user string) (string, llm.Usage, error) {
	if model == "" {
		model = haikuModelID
	}
	text, u, err := visionExtractModel(ctx, a.APIKey, model, img, mime, system, user)
	return text, llm.Usage{Provider: "anthropic", Model: model, InputTokens: int64(u.InputTokens), OutputTokens: int64(u.OutputTokens), ImageCount: 1}, err
}

// VisionWith runs a vision prompt through any llm.Provider and applies the same JSON
// sanitization as the legacy path, so callers can switch provider without behaviour change.
func VisionWith(ctx context.Context, p llm.Provider, model string, img []byte, mime, system, user string) (string, llm.Usage, error) {
	text, u, err := p.Vision(ctx, model, img, normalizeImageMediaType(mime), system, user)
	if err != nil {
		return "", u, err
	}
	return SanitizeVisionJSON(text), u, nil
}
