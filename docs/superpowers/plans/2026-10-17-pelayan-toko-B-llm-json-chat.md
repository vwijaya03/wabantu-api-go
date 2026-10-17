# Pelayan Toko — Part B: JSON chat completion in `shared/llm` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the new LLM-first pipeline make one chain-aware chat call that asks Gemini for strict JSON with a temperature, and that reports finish reason, thinking tokens and cached tokens. Anthropic degrades gracefully: prompt-only JSON, temperature honoured.

**Architecture:** The change is additive.
- New optional fields on `llm.ChatRequest` and `llm.Usage`.
- A `chatGenCfg` builder for Gemini.
- A chain-aware `llm.RunChat` next to `llm.RunToolChat`.
- Anthropic `Chat` honours `Temperature`.

No interface changes, so every existing `Provider` implementation keeps compiling.

**Tech Stack:** Go 1.26; `go test ./shared/llm/` (plain go test works); the `ai` package compiles with `go build ./ai/` and its tests need `encore test ./ai/` (Docker is up).

Spec: `docs/superpowers/specs/2026-10-17-pelayan-toko-llm-first-design.md` §3.1, §3.2 (`shared/llm` bullet).

## Global Constraints

- Worktree `/Users/viko/Documents/WABantu/.worktrees/api-go-pt-b-llm`, branch `feat/pt-b-llm` (from `feat/pelayan-toko-llm-first`). Only touch `shared/llm/*` and `ai/llm_provider.go`.
- Do not change the `Provider`, `ToolCaller` or `Registry` interfaces/signatures. Do not change `genCfg` behaviour for existing callers (`Complete`, `ChatWithTools`).
- The Gemini API key is sent only in the `x-goog-api-key` header, never in the URL (existing behaviour; keep it).
- Commit messages in Indonesian, conventional prefix, trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. `set -o pipefail` when piping test output. Never print secrets; do not push.

---

### Task 1: ChatRequest JSON/temperature and Gemini generationConfig

**Files:**
- Modify: `shared/llm/llm.go`. In `ChatRequest` (≈:40), add the fields. In `Usage` (≈:27), add the fields.
- Modify: `shared/llm/gemini.go`:
  - `gResp` (≈:58-74): decode `finishReason`, `thoughtsTokenCount` and `cachedContentTokenCount`.
  - `generate` (≈:147-170): fill the new `Usage` fields.
  - `Chat` (≈:187): use `chatGenCfg`.
- Test: `shared/llm/chat_json_test.go` (new)

**Interfaces:**
- Produces:
  - `ChatRequest.JSON bool`, `ChatRequest.Temperature *float64`
  - `Usage.ThoughtsTokens int64`, `Usage.CachedTokens int64`, `Usage.FinishReason string`
  - `func chatGenCfg(r ChatRequest) map[string]any` (unexported)

- [ ] **Step 1: Write the failing test** `shared/llm/chat_json_test.go`:
```go
package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func geminiJSONServer(t *testing.T, resp string, gotBody *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") == "" || r.URL.Query().Get("key") != "" {
			t.Errorf("api key must be sent only as header")
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const okJSONResp = `{"candidates":[{"content":{"role":"model","parts":[{"text":"{\"reply\":\"hi\"}"}]},"finishReason":"STOP"}],
"usageMetadata":{"promptTokenCount":120,"candidatesTokenCount":15,"thoughtsTokenCount":7,"cachedContentTokenCount":100}}`

func TestGeminiChatJSONModeAndTemperature(t *testing.T) {
	var body map[string]any
	srv := geminiJSONServer(t, okJSONResp, &body)
	g := NewGeminiProvider("fake-key")
	g.BaseURL = srv.URL
	temp := 0.3
	resp, err := g.Chat(context.Background(), ChatRequest{
		Model: GeminiDefaultModel, System: "sys",
		Messages:  []Message{{Role: "user", Text: "hello"}},
		MaxTokens: 1024, JSON: true, Temperature: &temp,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := body["generationConfig"].(map[string]any)
	if cfg["responseMimeType"] != "application/json" {
		t.Errorf("responseMimeType = %v", cfg["responseMimeType"])
	}
	if cfg["temperature"] != 0.3 {
		t.Errorf("temperature = %v", cfg["temperature"])
	}
	if cfg["maxOutputTokens"] != float64(1024) {
		t.Errorf("maxOutputTokens = %v", cfg["maxOutputTokens"])
	}
	if resp.Text != `{"reply":"hi"}` {
		t.Errorf("text = %q", resp.Text)
	}
	u := resp.Usage
	if u.InputTokens != 120 || u.OutputTokens != 15 || u.ThoughtsTokens != 7 || u.CachedTokens != 100 || u.FinishReason != "STOP" {
		t.Errorf("usage = %+v", u)
	}
}

func TestGeminiChatWithoutJSONKeepsOldConfig(t *testing.T) {
	var body map[string]any
	srv := geminiJSONServer(t, okJSONResp, &body)
	g := NewGeminiProvider("fake-key")
	g.BaseURL = srv.URL
	if _, err := g.Chat(context.Background(), ChatRequest{Model: GeminiDefaultModel, Messages: []Message{{Role: "user", Text: "x"}}, MaxTokens: 50}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := body["generationConfig"].(map[string]any)
	if _, ok := cfg["responseMimeType"]; ok {
		t.Error("responseMimeType must be absent when JSON=false")
	}
	if _, ok := cfg["temperature"]; ok {
		t.Error("temperature must be absent when nil")
	}
	if cfg["maxOutputTokens"] != float64(50) {
		t.Errorf("maxOutputTokens = %v", cfg["maxOutputTokens"])
	}
}

func TestGeminiFinishReasonOnTruncation(t *testing.T) {
	var body map[string]any
	srv := geminiJSONServer(t, `{"candidates":[{"content":{"parts":[{"text":"{\"reply\":\"cut"}]},"finishReason":"MAX_TOKENS"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":300}}`, &body)
	g := NewGeminiProvider("fake-key")
	g.BaseURL = srv.URL
	resp, err := g.Chat(context.Background(), ChatRequest{Model: GeminiDefaultModel, Messages: []Message{{Role: "user", Text: "x"}}, JSON: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Usage.FinishReason != "MAX_TOKENS" {
		t.Fatalf("finishReason = %q", resp.Usage.FinishReason)
	}
}
```

- [ ] **Step 2: Run it**

Run: `cd /Users/viko/Documents/WABantu/.worktrees/api-go-pt-b-llm && go test ./shared/llm/ -run 'GeminiChatJSON|GeminiChatWithoutJSON|GeminiFinishReason' -count=1`
Expected: FAIL to compile (unknown fields `JSON`, `Temperature`, `ThoughtsTokens`, …). If `GeminiProvider` has no `BaseURL` field under that name, use the field the existing `geminiMock` helper in `shared/llm/llm_test.go` overrides.

- [ ] **Step 3: Implement**

`shared/llm/llm.go`, add the fields (keep existing ones and order):
```go
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
```
In `Usage` add:
```go
	// Gemini extras (0/"" for other providers).
	ThoughtsTokens int64
	CachedTokens   int64
	FinishReason   string
```
`shared/llm/gemini.go`:
- In `gResp`, give the candidate struct a `FinishReason string \`json:"finishReason"\``.
- Give `UsageMetadata` the fields `ThoughtsTokenCount int64 \`json:"thoughtsTokenCount"\`` and `CachedContentTokenCount int64 \`json:"cachedContentTokenCount"\``.
- In `generate`, after setting `u.InputTokens/u.OutputTokens`, add:
```go
	u.ThoughtsTokens = gr.UsageMetadata.ThoughtsTokenCount
	u.CachedTokens = gr.UsageMetadata.CachedContentTokenCount
	if len(gr.Candidates) > 0 {
		u.FinishReason = gr.Candidates[0].FinishReason
	}
```
- Add `chatGenCfg` and use it in `Chat` (`GenerationConfig: chatGenCfg(r)` instead of `genCfg(r.MaxTokens)`):
```go
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
```

- [ ] **Step 4: Run** `set -o pipefail; go test ./shared/llm/ -count=1 2>&1 | tail -20`. Expected: PASS, with all existing tests still green.

- [ ] **Step 5: Commit** `feat(llm): chat Gemini mendukung JSON mode, temperature, dan finishReason`, with the trailer.

---

### Task 2: Chain-aware `RunChat` and Anthropic temperature

**Files:**
- Create: `shared/llm/run_chat.go`
- Modify: `ai/llm_provider.go` (`AnthropicProvider.Chat` ≈:33-80): honour `Temperature`.
- Test: `shared/llm/run_chat_test.go` (new)

**Interfaces:**
- Produces: `func RunChat(ctx context.Context, reg *Registry, tenantID string, fn Function, req ChatRequest) (ChatResponse, RunResult, error)`. It walks the tenant's chain like `RunToolChat`, sets `req.Model` per link, and calls `Provider.Chat`.

- [ ] **Step 1: Write the failing test** `shared/llm/run_chat_test.go`. Use the registry helpers already in the package. `regWith`/`chatChain` are package-private helpers in `run_test.go`/`tools_test.go`. Read them first and build the registry the same way. The test must assert:
  1. With a chain `[mockA(model "m1")]`, `RunChat` returns mockA's reply. The `ChatRequest` the provider receives has `Model == "m1"`, `JSON == true` and the same `Temperature` pointer value.
  2. With a chain `[failing(429 APIError), mockB]`, `RunChat` falls back to mockB and `RunResult.Attempts == 2`.

  If `MockProvider` cannot record the request or return an error, add a small test-only provider in the test file:
```go
type recordingChat struct {
	MockProvider
	got  ChatRequest
	err  error
	text string
}

func (p *recordingChat) Chat(ctx context.Context, r ChatRequest) (ChatResponse, error) {
	p.got = r
	if p.err != nil {
		return ChatResponse{}, p.err
	}
	return ChatResponse{Text: p.text, Usage: Usage{Provider: p.ID(), Model: r.Model}}, nil
}
```
  Use `&APIError{StatusCode: 429}`, or whatever the retryable error type in `retry.go` is called, for the failing link. Set `MockProvider.Name` so `ID()` matches the chain provider ids.

- [ ] **Step 2: Run** `go test ./shared/llm/ -run RunChat -count=1`. Expected: FAIL (`undefined: RunChat`).

- [ ] **Step 3: Implement** `shared/llm/run_chat.go`:
```go
package llm

import "context"

// RunChat runs a plain chat completion through the tenant's chain for fn (tenant row → global
// row → code default), advancing on retryable errors or link timeouts like RunToolChat.
func RunChat(ctx context.Context, reg *Registry, tenantID string, fn Function, req ChatRequest) (ChatResponse, RunResult, error) {
	var out ChatResponse
	res, err := reg.Run(ctx, tenantID, fn, func(ctx context.Context, p Provider, model string) error {
		r := req
		r.Model = model
		var e error
		out, e = p.Chat(ctx, r)
		return e
	})
	return out, res, err
}
```
In `ai/llm_provider.go` `AnthropicProvider.Chat`, set the temperature on the SDK params when `r.Temperature != nil`. Use the same SDK helper `aivision/vision.go:224` uses, for example `params.Temperature = anthropic.Float(*r.Temperature)`. JSON stays prompt-only.

- [ ] **Step 4: Verify**
- Run: `set -o pipefail; go test ./shared/llm/ -count=1 2>&1 | tail -20` → PASS.
- Run: `go build ./ai/ ./shared/... && go vet ./shared/llm/` → clean.
- Optionally run `encore test ./ai/ -run 'TestAnthropicProvider' -count=1` (needs Docker; expect PASS). If `encore test` cannot start in this environment, say so in the report instead of skipping silently.

- [ ] **Step 5: Commit** `feat(llm): RunChat lewat rantai fungsi dan temperature Anthropic`, with the trailer.
