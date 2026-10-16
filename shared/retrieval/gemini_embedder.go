package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"encore.app/wabantu/shared/llm"
)

const (
	geminiEmbedBaseURL   = "https://generativelanguage.googleapis.com/v1beta"
	geminiEmbedTimeout   = 8 * time.Second
	geminiEmbedBatchMax  = 100 // batchEmbedContents limit
	geminiEmbedMaxBytes  = 8192
	geminiEmbedMaxResp   = 16 << 20
	geminiAPIKeyEnvName  = "GeminiAPIKey" // process env (Cloud Run secret → env), same as the ai package
	geminiEmbedOutputDim = llm.EmbeddingDims
)

// GeminiEmbedder calls gemini-embedding-001 with outputDimensionality=1536 so vectors fit the
// existing Pinecone index. Truncated (<3072) outputs are not unit-length, so they are L2-normalised.
type GeminiEmbedder struct {
	apiKey  string
	baseURL string
	http    *http.Client
	model   string
	dims    int
}

func NewGeminiEmbedder(apiKey string) *GeminiEmbedder {
	return &GeminiEmbedder{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: geminiEmbedBaseURL,
		http:    &http.Client{Timeout: geminiEmbedTimeout},
		model:   llm.GeminiEmbeddingModel,
		dims:    geminiEmbedOutputDim,
	}
}

// GeminiAPIKeyFromEnv reads the optional Gemini key from the process environment.
func GeminiAPIKeyFromEnv() string { return strings.TrimSpace(os.Getenv(geminiAPIKeyEnvName)) }

func (e *GeminiEmbedder) Dimensions() int { return e.dims }

func (e *GeminiEmbedder) Model() string { return e.model }

type geminiEmbedReq struct {
	Model                string             `json:"model"`
	Content              geminiEmbedContent `json:"content"`
	OutputDimensionality int                `json:"outputDimensionality"`
}

type geminiEmbedContent struct {
	Parts []geminiEmbedPart `json:"parts"`
}

type geminiEmbedPart struct {
	Text string `json:"text"`
}

type geminiEmbedResp struct {
	Embeddings []struct {
		Values []float32 `json:"values"`
	} `json:"embeddings"`
	Error *struct {
		Status string `json:"status"`
	} `json:"error"`
}

func prepareGeminiText(s string) string {
	s = SanitizeForEmbed(s)
	if len(s) > geminiEmbedMaxBytes {
		cut := geminiEmbedMaxBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	if strings.TrimSpace(s) == "" {
		return " "
	}
	return s
}

func (e *GeminiEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil || e.apiKey == "" {
		return nil, fmt.Errorf("gemini embedder not configured")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	if err := llm.ValidateModel("gemini", e.model); err != nil {
		return nil, err
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += geminiEmbedBatchMax {
		end := start + geminiEmbedBatchMax
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := e.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (e *GeminiEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqs := make([]geminiEmbedReq, len(texts))
	for i, t := range texts {
		reqs[i] = geminiEmbedReq{
			Model:                "models/" + e.model,
			Content:              geminiEmbedContent{Parts: []geminiEmbedPart{{Text: prepareGeminiText(t)}}},
			OutputDimensionality: e.dims,
		}
	}
	body, err := json.Marshal(map[string]any{"requests": reqs})
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(e.baseURL, "/") + "/models/" + e.model + ":batchEmbedContents"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", e.apiKey) // header only: never in URL or logs
	hc := e.http
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini embeddings: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, geminiEmbedMaxResp))
	if err != nil {
		return nil, fmt.Errorf("gemini embeddings: read response: %w", err)
	}
	var parsed geminiEmbedResp
	_ = json.Unmarshal(raw, &parsed)
	if resp.StatusCode/100 != 2 {
		// Status code + Google status enum only; the body may echo request details.
		status := ""
		if parsed.Error != nil {
			status = " " + parsed.Error.Status
		}
		return nil, fmt.Errorf("gemini embeddings: http %d%s", resp.StatusCode, status)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("gemini embeddings: expected %d vectors, got %d", len(texts), len(parsed.Embeddings))
	}
	out := make([][]float32, len(parsed.Embeddings))
	for i, em := range parsed.Embeddings {
		if len(em.Values) != e.dims {
			return nil, fmt.Errorf("gemini embedding %d: got %d dims, want %d", i, len(em.Values), e.dims)
		}
		normalize(em.Values)
		out[i] = em.Values
	}
	return out, nil
}
