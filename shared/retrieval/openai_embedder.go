package retrieval

import (
	"context"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// OpenAIEmbedder calls OpenAI embeddings API.
type OpenAIEmbedder struct {
	client *openai.Client
	model  string
	dims   int
}

func NewOpenAIEmbedder(apiKey string) *OpenAIEmbedder {
	return &OpenAIEmbedder{
		client: openai.NewClient(apiKey),
		model:  EmbeddingModel,
		dims:   EmbeddingDims,
	}
}

func (e *OpenAIEmbedder) Dimensions() int { return e.dims }

func (e *OpenAIEmbedder) Model() string { return e.model }

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil || e.client == nil {
		return nil, fmt.Errorf("openai embedder not configured")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequestStrings{
		Input: texts,
		Model: openai.EmbeddingModel(e.model),
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("openai embeddings: expected %d vectors, got %d", len(texts), len(resp.Data))
	}
	out := make([][]float32, len(resp.Data))
	for i, d := range resp.Data {
		if len(d.Embedding) != e.dims {
			return nil, fmt.Errorf("openai embedding %d: got %d dims, want %d", i, len(d.Embedding), e.dims)
		}
		out[i] = d.Embedding
	}
	return out, nil
}

// Configured returns true when API key is non-empty.
func OpenAIConfigured(apiKey string) bool {
	return strings.TrimSpace(apiKey) != ""
}
