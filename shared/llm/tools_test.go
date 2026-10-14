package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

var catalogTools = []ToolDef{{
	Name: "search_catalog", Description: "Cari produk",
	Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "$schema": "x",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
			"tags":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"query"},
	},
}}

func TestGeminiSchemaConversion(t *testing.T) {
	g := GeminiSchema(catalogTools[0].Parameters)
	if g["type"] != "OBJECT" || g["additionalProperties"] != nil || g["$schema"] != nil {
		t.Fatalf("%v", g)
	}
	props := g["properties"].(map[string]any)
	if props["query"].(map[string]any)["type"] != "STRING" ||
		props["tags"].(map[string]any)["items"].(map[string]any)["type"] != "STRING" {
		t.Fatalf("%v", props)
	}
	if catalogTools[0].Parameters["type"] != "object" {
		t.Fatal("input mutated")
	}
}

// fakeGemini scripts generateContent responses and records request bodies.
type fakeGemini struct {
	mu    sync.Mutex
	reqs  []map[string]any
	resps []string
}

func (f *fakeGemini) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	f.mu.Lock()
	f.reqs = append(f.reqs, m)
	i := len(f.reqs) - 1
	resp := f.resps[len(f.resps)-1]
	if i < len(f.resps) {
		resp = f.resps[i]
	}
	f.mu.Unlock()
	if !strings.HasSuffix(r.URL.Path, "/models/gemini-2.5-flash:generateContent") || r.Header.Get("x-goog-api-key") != "k" {
		http.Error(w, "bad", 400)
		return
	}
	_, _ = io.WriteString(w, resp)
}

const callResp = `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"search_catalog","args":{"query":"kopi"}},"thoughtSignature":"sig1"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3}}`
const textResp = `{"candidates":[{"content":{"role":"model","parts":[{"text":"Kopi Gayo Rp85.000"}]}}],"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":5}}`

func newGeminiTest(t *testing.T, resps ...string) (*GeminiProvider, *fakeGemini) {
	f := &fakeGemini{resps: resps}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	g := NewGeminiProvider("k")
	g.BaseURL = srv.URL
	return g, f
}

func TestGeminiToolLoop(t *testing.T) {
	g, f := newGeminiTest(t, callResp, textResp)
	var gotArgs string
	out, err := g.ChatWithTools(context.Background(), ToolChatRequest{
		Model: "gemini-2.5-flash", System: "sys", Tools: catalogTools,
		Messages: []Message{{Role: "user", Text: "ada kopi?"}},
	}, func(name string, args json.RawMessage) (string, error) {
		gotArgs = name + string(args)
		return `{"items":[{"name":"Kopi Gayo","price":85000}]}`, nil
	})
	if err != nil || out.Text != "Kopi Gayo Rp85.000" || !out.UsedTools || out.Rounds != 2 {
		t.Fatalf("%+v %v", out, err)
	}
	if out.Usage.InputTokens != 30 || out.Usage.OutputTokens != 8 || out.Usage.Model != "gemini-2.5-flash" {
		t.Fatalf("usage %+v", out.Usage)
	}
	if gotArgs != `search_catalog{"query":"kopi"}` {
		t.Fatal(gotArgs)
	}
	// Round 1 declares tools in Gemini format.
	decl := f.reqs[0]["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
	if decl["name"] != "search_catalog" || decl["parameters"].(map[string]any)["type"] != "OBJECT" {
		t.Fatalf("%v", decl)
	}
	// Round 2 echoes the model turn (incl. thoughtSignature) and sends functionResponse.
	c := f.reqs[1]["contents"].([]any)
	if len(c) != 3 {
		t.Fatalf("contents=%d", len(c))
	}
	model := c[1].(map[string]any)
	mp := model["parts"].([]any)[0].(map[string]any)
	if model["role"] != "model" || mp["thoughtSignature"] != "sig1" || mp["functionCall"] == nil {
		t.Fatalf("%v", model)
	}
	fr := c[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "search_catalog" || fr["response"].(map[string]any)["items"] == nil {
		t.Fatalf("%v", fr)
	}
}

func TestGeminiToolErrorAndRoundLimit(t *testing.T) {
	g, f := newGeminiTest(t, callResp)
	_, err := g.ChatWithTools(context.Background(), ToolChatRequest{Model: "gemini-2.5-flash", Tools: catalogTools, MaxRounds: 2,
		Messages: []Message{{Role: "user", Text: "x"}}},
		func(string, json.RawMessage) (string, error) { return "", errors.New("boom") })
	if err == nil || !strings.Contains(err.Error(), "exceeded 2 rounds") {
		t.Fatal(err)
	}
	fr := f.reqs[1]["contents"].([]any)[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["response"].(map[string]any)["error"] != "boom" {
		t.Fatalf("%v", fr)
	}
}

func TestGeminiToolLoopHTTPErrorRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "UNAVAILABLE", 503) }))
	defer srv.Close()
	g := NewGeminiProvider("k")
	g.BaseURL = srv.URL
	_, err := g.ChatWithTools(context.Background(), ToolChatRequest{Model: "gemini-2.5-flash", Messages: []Message{{Role: "user", Text: "x"}}}, nil)
	if !IsProviderRetryable(err) {
		t.Fatal(err)
	}
}

var chatChain = []ChainLink{{Provider: "gemini", Model: "gemini-2.5-flash"}, {Provider: "anthropic", Model: "claude-haiku-4-5"}}

// anthropicSpecial mimics ai's legacy Anthropic path (ResolveRouting picks the model when the link has none).
func anthropicSpecial(routeModel string, calls *[]string) ToolLink {
	return func(_ context.Context, p Provider, model string) (ToolChatResponse, bool, error) {
		if p.ID() != "anthropic" {
			return ToolChatResponse{}, false, nil
		}
		if model == "" {
			model = routeModel
		}
		*calls = append(*calls, model)
		return ToolChatResponse{Text: "dari claude", Usage: Usage{Provider: "anthropic", Model: model}}, true, nil
	}
}

func TestRunToolChatGeminiFirst(t *testing.T) {
	g := &MockToolProvider{MockProvider: &MockProvider{Name: "gemini"}, Script: func(r ToolChatRequest, exec ToolExecutor) (ToolChatResponse, error) {
		res, _ := exec("search_catalog", json.RawMessage(`{"query":"kopi"}`))
		return ToolChatResponse{Text: "gemini:" + res, UsedTools: true}, nil
	}}
	var ac []string
	a := &MockProvider{Name: "anthropic"}
	out, res, err := RunToolChat(context.Background(), regWith(chatChain, FnChat, g, a), "t1", FnChat,
		ToolChatRequest{Tools: catalogTools}, func(string, json.RawMessage) (string, error) { return "ok", nil },
		anthropicSpecial("claude-sonnet-4-5", &ac))
	if err != nil || out.Text != "gemini:ok" || out.Usage.Model != "gemini-2.5-flash" || res.Provider != "gemini" || len(ac) != 0 {
		t.Fatalf("%+v %+v %v %v", out, res, err, ac)
	}
	if g.LastReq.Model != "gemini-2.5-flash" || len(g.LastReq.Tools) != 1 {
		t.Fatalf("%+v", g.LastReq)
	}
}

func TestRunToolChatFallbackToAnthropic(t *testing.T) {
	g := &MockToolProvider{MockProvider: &MockProvider{Name: "gemini", Err: &APIError{Status: 503}}}
	var ac []string
	out, res, err := RunToolChat(context.Background(), regWith(chatChain, FnChat, g, &MockProvider{Name: "anthropic"}), "t1", FnChat,
		ToolChatRequest{}, nil, anthropicSpecial("claude-sonnet-4-5", &ac))
	if err != nil || out.Text != "dari claude" || res.Provider != "anthropic" || res.Attempts != 2 || out.Usage.Model != "claude-haiku-4-5" {
		t.Fatalf("%+v %+v %v", out, res, err)
	}
}

func TestRunToolChatDefaultIsAnthropicWithRouting(t *testing.T) {
	// No DB config → code default chain (anthropic, no model) → ResolveRouting model is used.
	r := NewRegistry(NewResolver(NewMemoryStore(), 0))
	r.Register(&MockToolProvider{MockProvider: &MockProvider{Name: "gemini"}})
	r.Register(&MockProvider{Name: "anthropic"})
	var ac []string
	out, res, err := RunToolChat(context.Background(), r, "t1", FnChat, ToolChatRequest{}, nil, anthropicSpecial("claude-sonnet-4-5", &ac))
	if err != nil || res.Provider != "anthropic" || out.Usage.Model != "claude-sonnet-4-5" {
		t.Fatalf("%+v %+v %v", out, res, err)
	}
}

func TestRunToolChatSkipsProviderWithoutTools(t *testing.T) {
	plain := &MockProvider{Name: "gemini", Reply: "x"} // not a ToolCaller
	var ac []string
	out, res, err := RunToolChat(context.Background(), regWith(chatChain, FnChat, plain, &MockProvider{Name: "anthropic"}), "t1", FnChat,
		ToolChatRequest{}, nil, anthropicSpecial("", &ac))
	if err != nil || res.Provider != "anthropic" || out.Text != "dari claude" {
		t.Fatalf("%+v %+v %v", out, res, err)
	}
}

func TestRunToolChatNoFallbackOnPermanent(t *testing.T) {
	g := &MockToolProvider{MockProvider: &MockProvider{Name: "gemini", Err: &APIError{Status: 400}}}
	var ac []string
	_, _, err := RunToolChat(context.Background(), regWith(chatChain, FnChat, g, &MockProvider{Name: "anthropic"}), "t1", FnChat,
		ToolChatRequest{}, nil, anthropicSpecial("", &ac))
	if err == nil || len(ac) != 0 {
		t.Fatalf("err=%v anthropic=%v", err, ac)
	}
}
