package aivision

import (
	"context"
	"fmt"
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

// visionViaRegistry resolves provider+model for fn and runs the vision prompt.
// With empty config this is exactly the legacy path (Anthropic Haiku snapshot).
func visionViaRegistry(ctx context.Context, fn llm.Function, apiKey string, img []byte, mime, system, user string) (string, Usage, error) {
	p, model, err := visionRegistry(apiKey).For(ctx, "", fn)
	if err != nil {
		return "", Usage{}, err
	}
	var text string
	var u llm.Usage
	if av, ok := p.(AnthropicVision); ok {
		// Legacy error messages / validation preserved verbatim.
		if model == "" {
			model = haikuModelID
		}
		var lu Usage
		text, lu, err = visionExtractModel(ctx, av.APIKey, model, img, mime, system, user)
		return text, lu, err
	}
	if len(img) == 0 {
		return "", Usage{}, fmt.Errorf("empty image")
	}
	text, u, err = VisionWith(ctx, p, model, img, mime, system, user)
	return text, Usage{InputTokens: int(u.InputTokens), OutputTokens: int(u.OutputTokens)}, err
}
