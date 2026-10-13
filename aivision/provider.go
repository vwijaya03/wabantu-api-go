package aivision

import (
	"context"
	"fmt"
	"strings"
	"sync"

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

// visionRegistry holds non-Anthropic vision providers (e.g. Gemini, registered at startup in BE-04)
// and the config resolver. Anthropic is bound per call because callers pass the API key.
var (
	visionMu       sync.RWMutex
	visionExtra    = map[string]llm.Provider{}
	visionResolver = llm.NewResolver(nil, 0) // nil store = code defaults (Anthropic) until BE-03 store is wired
)

// RegisterVisionProvider adds a provider usable by vision/OCR config chains.
func RegisterVisionProvider(p llm.Provider) {
	visionMu.Lock()
	visionExtra[p.ID()] = p
	visionMu.Unlock()
}

// SetVisionResolver swaps the config resolver (e.g. DB-backed).
func SetVisionResolver(r *llm.Resolver) {
	visionMu.Lock()
	visionResolver = r
	visionMu.Unlock()
}

func visionRegistry(apiKey string) *llm.Registry {
	visionMu.RLock()
	defer visionMu.RUnlock()
	reg := llm.NewRegistry(visionResolver)
	for _, p := range visionExtra {
		reg.Register(p)
	}
	reg.Register(AnthropicVision{APIKey: apiKey})
	return reg
}

// visionViaRegistry runs the vision prompt over the configured chain for (tenant, fn) with
// BE-05 fallback on retryable errors. With empty config this is exactly the legacy path.
func visionViaRegistry(ctx context.Context, fn llm.Function, tenantID, apiKey string, img []byte, mime, system, user string) (string, Usage, error) {
	var (
		text string
		out  Usage
	)
	err := visionRegistry(apiKey).Run(ctx, tenantID, fn, func(p llm.Provider, model string) error {
		if av, ok := p.(AnthropicVision); ok {
			// Legacy error messages / validation preserved verbatim.
			if model == "" {
				model = haikuModelID
			}
			t, lu, err := visionExtractModel(ctx, av.APIKey, model, img, mime, system, user)
			lu.Provider, lu.Model = "anthropic", model
			text, out = t, lu
			return err
		}
		if len(img) == 0 {
			return fmt.Errorf("empty image")
		}
		t, u, err := VisionWith(ctx, p, model, img, mime, system, user)
		text = t
		out = Usage{InputTokens: int(u.InputTokens), OutputTokens: int(u.OutputTokens), Provider: u.Provider, Model: u.Model}
		return err
	})
	if err != nil {
		return "", out, err
	}
	return text, out, nil
}

// IsRetryable classifies transient Anthropic vision errors for chain fallback.
func (AnthropicVision) IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, p := range []string{"429", "529", "500", "502", "503", "504", "overloaded", "too many requests", "timeout", "deadline exceeded", "connection reset", "connection refused", "eof"} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
