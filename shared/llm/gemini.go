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

func NewGeminiProvider(apiKey string) *GeminiProvider {
	return &GeminiProvider{
		APIKey:       apiKey,
		BaseURL:      "https://generativelanguage.googleapis.com/v1beta",
		HTTP:         &http.Client{Timeout: 20 * time.Second},
		DefaultModel: "gemini-2.5-flash",
	}
}

func (g *GeminiProvider) ID() string { return "gemini" }

type gPart struct {
	Text       string    `json:"text,omitempty"`
	InlineData *gInlined `json:"inlineData,omitempty"`
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
	GenerationConfig  map[string]any `json:"generationConfig,omitempty"`
}
type gResp struct {
	Candidates []struct {
		Content gContent `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int64 `json:"promptTokenCount"`
		CandidatesTokenCount int64 `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}

// APIError carries HTTP status for retry classification (BE-05).
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return fmt.Sprintf("gemini: http %d: %s", e.Status, e.Body) }

// Retryable reports 429/5xx.
func (e *APIError) Retryable() bool { return e.Status == 429 || e.Status >= 500 }

func (g *GeminiProvider) model(m string) string {
	if strings.TrimSpace(m) == "" {
		return g.DefaultModel
	}
	return m
}

func (g *GeminiProvider) generate(ctx context.Context, model string, body gReq) (string, Usage, error) {
	model = g.model(model)
	u := Usage{Provider: g.ID(), Model: model}
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
		return "", u, fmt.Errorf("gemini: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return "", u, &APIError{Status: resp.StatusCode, Body: string(raw)}
	}
	var gr gResp
	if err := json.Unmarshal(raw, &gr); err != nil {
		return "", u, fmt.Errorf("gemini: decode: %w", err)
	}
	u.InputTokens = gr.UsageMetadata.PromptTokenCount
	u.OutputTokens = gr.UsageMetadata.CandidatesTokenCount
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

func (g *GeminiProvider) Chat(ctx context.Context, r ChatRequest) (ChatResponse, error) {
	body := gReq{SystemInstruction: sysContent(r.System), GenerationConfig: genCfg(r.MaxTokens)}
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
		GenerationConfig: map[string]any{"temperature": 0.1},
	})
	u.ImageCount = 1
	return text, u, err
}
