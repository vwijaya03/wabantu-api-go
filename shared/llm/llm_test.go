package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolverFallbackOrder(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	r := NewResolver(st, time.Minute)

	// empty tables → code default (anthropic)
	c, err := r.Resolve(ctx, FnChat, "t1")
	if err != nil || c.Chain[0].Provider != "anthropic" {
		t.Fatalf("want anthropic default, got %+v %v", c, err)
	}

	st.Set(FunctionConfig{Function: FnChat, Chain: []ChainLink{{Provider: "gemini", Model: "gemini-2.5-flash"}}})
	r.Invalidate()
	if c, _ = r.Resolve(ctx, FnChat, "t1"); c.Chain[0].Provider != "gemini" {
		t.Fatalf("want global gemini, got %+v", c)
	}

	st.Set(FunctionConfig{Function: FnChat, TenantID: "t1", Chain: []ChainLink{{Provider: "openai", Model: "x"}}})
	r.Invalidate()
	if c, _ = r.Resolve(ctx, FnChat, "t1"); c.Chain[0].Provider != "openai" {
		t.Fatalf("want tenant override, got %+v", c)
	}
	if c, _ = r.Resolve(ctx, FnChat, "t2"); c.Chain[0].Provider != "gemini" {
		t.Fatalf("other tenant should get global, got %+v", c)
	}
}

func TestResolverTTLSwitchWithoutRestart(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	r := NewResolver(st, DefaultTTL)
	now := time.Unix(1000, 0)
	r.now = func() time.Time { return now }

	c, _ := r.Resolve(ctx, FnOCR, "")
	if c.Chain[0].Provider != "anthropic" {
		t.Fatal("expected default")
	}
	st.Set(FunctionConfig{Function: FnOCR, Chain: []ChainLink{{Provider: "gemini"}}})
	if c, _ = r.Resolve(ctx, FnOCR, ""); c.Chain[0].Provider != "anthropic" {
		t.Fatal("should still be cached")
	}
	now = now.Add(DefaultTTL + time.Second)
	if c, _ = r.Resolve(ctx, FnOCR, ""); c.Chain[0].Provider != "gemini" {
		t.Fatalf("should switch after ≤30s TTL, got %+v", c)
	}
}

func TestResolverDisabledProviderFiltered(t *testing.T) {
	st := NewMemoryStore()
	st.Providers = map[string]bool{"gemini": false, "anthropic": true}
	st.Set(FunctionConfig{Function: FnChat, Chain: []ChainLink{{Provider: "gemini"}, {Provider: "anthropic", Model: "claude-haiku-4-5"}}})
	c, _ := NewResolver(st, 0).Resolve(context.Background(), FnChat, "")
	if len(c.Chain) != 1 || c.Chain[0].Provider != "anthropic" {
		t.Fatalf("got %+v", c)
	}
	st.Providers = map[string]bool{"gemini": false, "anthropic": false}
	c, _ = NewResolver(st, 0).Resolve(context.Background(), FnChat, "")
	if c.Chain[0].Provider != "anthropic" || c.Chain[0].Model != "" {
		t.Fatalf("all disabled → code default, got %+v", c)
	}
}

type errStore struct{}

func (errStore) LoadFunctionConfig(context.Context, Function, string) (FunctionConfig, bool, error) {
	return FunctionConfig{}, false, errors.New("db down")
}
func (errStore) EnabledProviders(context.Context) (map[string]bool, error) { return nil, nil }

func TestResolverDBErrorFallsBackToDefault(t *testing.T) {
	c, err := NewResolver(errStore{}, 0).Resolve(context.Background(), FnChat, "t")
	if err == nil || c.Chain[0].Provider != "anthropic" {
		t.Fatalf("got %+v %v", c, err)
	}
}

func TestRegistryFor(t *testing.T) {
	st := NewMemoryStore()
	st.Set(FunctionConfig{Function: FnChat, Chain: []ChainLink{{Provider: "gemini", Model: "gemini-2.5-flash"}, {Provider: "anthropic"}}})
	reg := NewRegistry(NewResolver(st, 0))
	a := &MockProvider{Name: "anthropic"}
	reg.Register(a)
	p, m, err := reg.For(context.Background(), "", FnChat)
	if err != nil || p.ID() != "anthropic" || m != "" {
		t.Fatalf("unregistered gemini should be skipped: %v %v %v", p, m, err)
	}
	reg.Register(&MockProvider{Name: "gemini"})
	p, m, _ = reg.For(context.Background(), "", FnChat)
	if p.ID() != "gemini" || m != "gemini-2.5-flash" {
		t.Fatalf("got %v %v", p.ID(), m)
	}
}

func TestParseChain(t *testing.T) {
	c, err := ParseChain([]byte(`[{"provider":"gemini","model":"gemini-2.5-flash","timeout_ms":8000},{"provider":"anthropic","model":"claude-haiku-4-5"}]`))
	if err != nil || len(c) != 2 || c[0].TimeoutMS != 8000 {
		t.Fatalf("%+v %v", c, err)
	}
}

func geminiMock(t *testing.T, status int, check func(path string, body map[string]any)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "fake-key" {
			t.Errorf("missing api key header")
		}
		if strings.Contains(r.URL.RawQuery, "fake-key") {
			t.Errorf("key leaked in URL")
		}
		raw, _ := io.ReadAll(r.Body)
		var b map[string]any
		_ = json.Unmarshal(raw, &b)
		if check != nil {
			check(r.URL.Path, b)
		}
		w.WriteHeader(status)
		if status != 200 {
			_, _ = w.Write([]byte(`{"error":{"message":"quota"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"halo kak"}]}}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":3}}`))
	}))
}

func TestGeminiComplete(t *testing.T) {
	srv := geminiMock(t, 200, func(path string, b map[string]any) {
		if path != "/models/"+GeminiDefaultModel+":generateContent" {
			t.Errorf("path %s", path)
		}
		if b["systemInstruction"] == nil {
			t.Errorf("system missing")
		}
	})
	defer srv.Close()
	g := NewGeminiProvider("fake-key")
	g.BaseURL = srv.URL
	text, u, err := g.Complete(context.Background(), "", "sys", "hai", 100)
	if err != nil || text != "halo kak" || u.InputTokens != 12 || u.OutputTokens != 3 || u.Provider != "gemini" || u.Model != GeminiDefaultModel {
		t.Fatalf("%q %+v %v", text, u, err)
	}
}

func TestGeminiChatRoles(t *testing.T) {
	srv := geminiMock(t, 200, func(_ string, b map[string]any) {
		cs := b["contents"].([]any)
		if len(cs) != 2 || cs[1].(map[string]any)["role"] != "model" {
			t.Errorf("roles not mapped: %v", cs)
		}
	})
	defer srv.Close()
	g := NewGeminiProvider("fake-key")
	g.BaseURL = srv.URL
	r, err := g.Chat(context.Background(), ChatRequest{Model: "gemini-2.5-flash", Messages: []Message{{Role: "user", Text: "a"}, {Role: "assistant", Text: "b"}}})
	if err != nil || r.Text != "halo kak" {
		t.Fatal(r, err)
	}
}

func TestGeminiVisionInlineImage(t *testing.T) {
	srv := geminiMock(t, 200, func(_ string, b map[string]any) {
		parts := b["contents"].([]any)[0].(map[string]any)["parts"].([]any)
		inl := parts[1].(map[string]any)["inlineData"].(map[string]any)
		if inl["mimeType"] != "image/png" || inl["data"] != "AQID" {
			t.Errorf("inline image wrong: %v", inl)
		}
	})
	defer srv.Close()
	g := NewGeminiProvider("fake-key")
	g.BaseURL = srv.URL
	_, u, err := g.Vision(context.Background(), "", []byte{1, 2, 3}, "image/png", "s", "u")
	if err != nil || u.ImageCount != 1 {
		t.Fatal(u, err)
	}
}

func TestGeminiErrors(t *testing.T) {
	srv := geminiMock(t, 429, nil)
	defer srv.Close()
	g := NewGeminiProvider("fake-key")
	g.BaseURL = srv.URL
	_, _, err := g.Complete(context.Background(), "", "", "x", 0)
	var ae *APIError
	if !errors.As(err, &ae) || !ae.Retryable() {
		t.Fatalf("want retryable APIError, got %v", err)
	}
	if _, _, err := NewGeminiProvider("").Complete(context.Background(), "", "", "x", 0); err == nil {
		t.Fatal("empty key must error")
	}
}
