package retrieval

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

const openAIHTTPTimeout = 5 * time.Second

// OpenAIEmbedder calls OpenAI embeddings API.
type OpenAIEmbedder struct {
	client     *openai.Client
	httpClient *http.Client
	model      string
	dims       int
}

func newOpenAIHTTPClient() *http.Client {
	return &http.Client{
		Timeout: openAIHTTPTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 5 * time.Second,
			ForceAttemptHTTP2:   true,
		},
	}
}

func NewOpenAIEmbedder(apiKey string) *OpenAIEmbedder {
	httpClient := newOpenAIHTTPClient()
	cfg := openai.DefaultConfig(apiKey)
	cfg.HTTPClient = httpClient
	return &OpenAIEmbedder{
		client:     openai.NewClientWithConfig(cfg),
		httpClient: httpClient,
		model:      EmbeddingModel,
		dims:       EmbeddingDims,
	}
}

// HTTPClient returns the underlying HTTP client (for connection warm-up).
func (e *OpenAIEmbedder) HTTPClient() *http.Client {
	if e == nil || e.httpClient == nil {
		return newOpenAIHTTPClient()
	}
	return e.httpClient
}

// WarmupOpenAIConnection pre-establishes TLS to OpenAI (GET /v1/models, no embedding cost).
func WarmupOpenAIConnection(apiKey string, client *http.Client) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return
	}
	if client == nil {
		client = newOpenAIHTTPClient()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.openai.com/v1/models", nil)
		if err != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := client.Do(req)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		_ = err
	}()
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
