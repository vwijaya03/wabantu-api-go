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
