package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GeminiProvider talks to the Gemini REST API (generativelanguage v1beta) with net/http,
// so it can be tested with httptest and needs no extra SDK. Tool-calling is BE-04.
type GeminiProvider struct {
	APIKey       string
	BaseURL      string // default https://generativelanguage.googleapis.com/v1beta
	HTTP         *http.Client
	DefaultModel string
}

// GeminiDefaultModel: cheapest stable Flash text model callable by new API keys
// (gemini-2.5-flash / -flash-lite return 404 "no longer available to new users", Oct 2026).
const GeminiDefaultModel = "gemini-3.1-flash-lite"

func NewGeminiProvider(apiKey string) *GeminiProvider {
	return &GeminiProvider{
		APIKey:       apiKey,
		BaseURL:      "https://generativelanguage.googleapis.com/v1beta",
		HTTP:         &http.Client{Timeout: 20 * time.Second},
		DefaultModel: GeminiDefaultModel,
	}
}

func (g *GeminiProvider) ID() string { return "gemini" }

// Available is false without an API key, so chains skip Gemini and fall back (logged).
func (g *GeminiProvider) Available() bool { return g != nil && strings.TrimSpace(g.APIKey) != "" }

type gPart struct {
	Text             string     `json:"text,omitempty"`
	InlineData       *gInlined  `json:"inlineData,omitempty"`
	FunctionCall     *gFuncCall `json:"functionCall,omitempty"`
	FunctionResponse *gFuncResp `json:"functionResponse,omitempty"`
	// ThoughtSignature must be echoed back unchanged on the model turn (Gemini 2.5 thinking).
	ThoughtSignature string `json:"thoughtSignature,omitempty"`
}
type gInlined struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}
type gContent struct {
	Role  string  `json:"role,omitempty"`
	Parts []gPart `json:"parts"`
}
type gReq struct {
	SystemInstruction *gContent      `json:"systemInstruction,omitempty"`
	Contents          []gContent     `json:"contents"`
	Tools             []gTool        `json:"tools,omitempty"`
	GenerationConfig  map[string]any `json:"generationConfig,omitempty"`

	rawOut *gResp // when set, generate stores the decoded response and skips text extraction
}
type gResp struct {
	Candidates []struct {
		Content      gContent `json:"content"`
		FinishReason string   `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount        int64 `json:"promptTokenCount"`
		CandidatesTokenCount    int64 `json:"candidatesTokenCount"`
		ThoughtsTokenCount      int64 `json:"thoughtsTokenCount"`
		CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
	} `json:"usageMetadata"`
}

// APIError carries HTTP status for retry classification (BE-05).
type APIError struct {
	Status int
	Body   string
}

// Error trims the upstream body: Gemini error payloads can be large and may echo request
// fragments, so logs only get a bounded prefix. The full body stays in e.Body.
func (e *APIError) Error() string {
	return fmt.Sprintf("gemini: http %d: %s", e.Status, TrimForLog(e.Body, maxErrBodyLog))
}

// Retryable reports 429/5xx.
func (e *APIError) Retryable() bool { return e.Status == 429 || e.Status >= 500 }

// IsRetryable implements RetryClassifier for Gemini (HTTP status + transport errors).
func (g *GeminiProvider) IsRetryable(err error) bool { return IsProviderRetryable(err) }

func (g *GeminiProvider) model(m string) string {
	if strings.TrimSpace(m) == "" {
		return g.DefaultModel
	}
	return m
}

func (g *GeminiProvider) generate(ctx context.Context, model string, body gReq) (string, Usage, error) {
	model = g.model(model)
	u := Usage{Provider: g.ID(), Model: model}
	// The model is interpolated into the URL path: only allowlisted ids are accepted.
	if err := ValidateModel(g.ID(), model); err != nil {
		return "", u, err
	}
	if IsEmbeddingModel(model) {
		return "", u, fmt.Errorf("gemini: model %q is embedding-only", model)
	}
	if strings.TrimSpace(g.APIKey) == "" {
		return "", u, fmt.Errorf("gemini: API key not configured")
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return "", u, err
	}
	url := strings.TrimRight(g.BaseURL, "/") + "/models/" + model + ":generateContent"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return "", u, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.APIKey) // header, never in URL/logs
	hc := g.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		// No response received: mark explicitly as transport so classification isn't heuristic.
		return "", u, &TransportError{Provider: "gemini", Err: err}
	}
	defer resp.Body.Close()
	raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if rerr != nil {
		if resp.StatusCode/100 != 2 {
			// HTTP status wins over the read error (a truncated 4xx must not look transient).
			return "", u, &APIError{Status: resp.StatusCode, Body: string(raw) + " [body read error: " + rerr.Error() + "]"}
		}
		// 2xx with truncated body: %w keeps io.ErrUnexpectedEOF visible → retryable.
		return "", u, fmt.Errorf("gemini: read body: %w", rerr)
	}
	if resp.StatusCode/100 != 2 {
		return "", u, &APIError{Status: resp.StatusCode, Body: string(raw)}
	}
	var gr gResp
	if err := json.Unmarshal(raw, &gr); err != nil {
		return "", u, fmt.Errorf("gemini: decode: %w", err)
	}
	u.InputTokens = gr.UsageMetadata.PromptTokenCount
	u.OutputTokens = gr.UsageMetadata.CandidatesTokenCount
	u.ThoughtsTokens = gr.UsageMetadata.ThoughtsTokenCount
	u.CachedTokens = gr.UsageMetadata.CachedContentTokenCount
	if len(gr.Candidates) > 0 {
		u.FinishReason = gr.Candidates[0].FinishReason
	}
	if body.rawOut != nil {
		*body.rawOut = gr
		return "", u, nil
	}
	var parts []string
	for _, c := range gr.Candidates {
		for _, p := range c.Content.Parts {
			if p.Text != "" {
				parts = append(parts, p.Text)
			}
		}
		break
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		return "", u, fmt.Errorf("gemini: empty completion")
	}
	return text, u, nil
}

func sysContent(s string) *gContent {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &gContent{Parts: []gPart{{Text: s}}}
}

func genCfg(maxTok int64) map[string]any {
	if maxTok <= 0 {
		return nil
	}
	return map[string]any{"maxOutputTokens": maxTok}
}

// chatGenCfg extends genCfg with JSON mode and temperature for Chat only.
func chatGenCfg(r ChatRequest) map[string]any {
	cfg := genCfg(r.MaxTokens)
	if !r.JSON && r.Temperature == nil {
		return cfg
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	if r.Temperature != nil {
		cfg["temperature"] = *r.Temperature
	}
	if r.JSON {
		cfg["responseMimeType"] = "application/json"
	}
	return cfg
}

func (g *GeminiProvider) Chat(ctx context.Context, r ChatRequest) (ChatResponse, error) {
	body := gReq{SystemInstruction: sysContent(r.System), GenerationConfig: chatGenCfg(r)}
	for _, m := range r.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		body.Contents = append(body.Contents, gContent{Role: role, Parts: []gPart{{Text: m.Text}}})
	}
	text, u, err := g.generate(ctx, r.Model, body)
	return ChatResponse{Text: text, Usage: u}, err
}

func (g *GeminiProvider) Complete(ctx context.Context, model, system, user string, maxTok int64) (string, Usage, error) {
	return g.generate(ctx, model, gReq{
		SystemInstruction: sysContent(system),
		Contents:          []gContent{{Role: "user", Parts: []gPart{{Text: user}}}},
		GenerationConfig:  genCfg(maxTok),
	})
}

func (g *GeminiProvider) Vision(ctx context.Context, model string, img []byte, mime, system, user string) (string, Usage, error) {
	if len(img) == 0 {
		return "", Usage{Provider: g.ID(), Model: g.model(model)}, fmt.Errorf("empty image")
	}
	text, u, err := g.generate(ctx, model, gReq{
		SystemInstruction: sysContent(system),
		Contents: []gContent{{Role: "user", Parts: []gPart{
			{Text: user},
			{InlineData: &gInlined{MimeType: mime, Data: base64.StdEncoding.EncodeToString(img)}},
		}}},
		// All vision prompts (payment proof, product match, imports) expect JSON.
		GenerationConfig: map[string]any{"temperature": 0.1, "responseMimeType": "application/json"},
	})
	u.ImageCount = 1
	return text, u, err
}
