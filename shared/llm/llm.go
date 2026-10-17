// Package llm is the provider-agnostic LLM abstraction (MIGRATION_PLAN §2).
// Concrete providers live next to their legacy clients (ai.AnthropicProvider,
// aivision.AnthropicVision) or in this package (GeminiProvider) to avoid import cycles.
package llm

import (
	"context"
	"errors"
)

// Function is the logical AI function a call is made for; config is resolved per function.
type Function string

const (
	FnChat      Function = "chat"
	FnIntent    Function = "intent"
	FnOCR       Function = "ocr"
	FnVision    Function = "vision"
	FnMemory    Function = "memory"
	FnEmbedding Function = "embedding"
	// FnPaymentOCR is payment-proof (bukti transfer) OCR, separate from generic vision so it
	// can be routed to Gemini for the demo independently of product-image matching.
	FnPaymentOCR Function = "payment_ocr"
)

// Usage reports real provider/model so usage accounting is not hardcoded.
type Usage struct {
	Provider, Model           string
	InputTokens, OutputTokens int64
	ImageCount                int64
	// Anthropic prompt-cache counters (0 for other providers).
	CacheReadInputTokens, CacheCreationInputTokens int64
	// Gemini extras (0/"" for other providers).
	ThoughtsTokens int64
	CachedTokens   int64
	FinishReason   string
}

type Message struct {
	Role string // "user" | "assistant"
	Text string
}

type ChatRequest struct {
	Model     string
	System    string
	Messages  []Message
	MaxTokens int64
	// JSON asks for a JSON-only reply. Gemini: responseMimeType=application/json.
	// Anthropic: ignored (callers instruct JSON in the prompt and parse defensively).
	JSON bool
	// Temperature: nil = provider default; pointer so 0 is expressible.
	Temperature *float64
}

type ChatResponse struct {
	Text  string
	Usage Usage
}

// Provider is implemented by every LLM backend.
type Provider interface {
	ID() string
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	Complete(ctx context.Context, model, system, user string, maxTok int64) (string, Usage, error)
	Vision(ctx context.Context, model string, img []byte, mime, system, user string) (string, Usage, error)
}

// ErrNotSupported is returned by providers that don't implement an operation.
var ErrNotSupported = errors.New("llm: operation not supported by provider")
