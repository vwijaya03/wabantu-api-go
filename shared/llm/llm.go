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
)

// Usage reports real provider/model so usage accounting is not hardcoded.
type Usage struct {
	Provider, Model           string
	InputTokens, OutputTokens int64
	ImageCount                int64
	// Anthropic prompt-cache counters (0 for other providers).
	CacheReadInputTokens, CacheCreationInputTokens int64
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
