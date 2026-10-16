package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateModel(t *testing.T) {
	ok := [][2]string{{"gemini", "gemini-2.5-flash"}, {"anthropic", "claude-haiku-4-5"}, {"anthropic", ""}, {"openai", "text-embedding-3-small"}}
	for _, c := range ok {
		if err := ValidateModel(c[0], c[1]); err != nil {
			t.Errorf("%v: %v", c, err)
		}
	}
	bad := [][2]string{
		{"gemini", "claude-haiku-4-5"},          // cross-provider
		{"anthropic", "gemini-2.5-flash"},       // cross-provider
		{"gemini", "../../v1/files"},            // path traversal into URL
		{"gemini", "gemini-2.5-flash:generate"}, // path injection
		{"gemini", "gemini-9-ultra"},            // not allowlisted
		{"nope", ""},
	}
	for _, c := range bad {
		if err := ValidateModel(c[0], c[1]); err == nil {
			t.Errorf("%v should be rejected", c)
		}
	}
}

func TestValidateFunctionConfig(t *testing.T) {
	good := []ChainLink{{Provider: "gemini", Model: "gemini-2.5-flash", TimeoutMS: 8000}, {Provider: "anthropic"}}
	if err := ValidateFunctionConfig(FnPaymentOCR, good, 1); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		fn    Function
		chain []ChainLink
		mr    int
	}{
		"unknown fn":       {"x", good, 1},
		"empty":            {FnChat, nil, 1},
		"too long":         {FnChat, []ChainLink{{Provider: "anthropic"}, {Provider: "gemini"}, {Provider: "anthropic", Model: "claude-haiku-4-5"}, {Provider: "gemini", Model: "gemini-2.5-pro"}}, 1},
		"embedding gemini": {FnEmbedding, []ChainLink{{Provider: "gemini"}}, 1},
		"bad timeout":      {FnChat, []ChainLink{{Provider: "gemini", TimeoutMS: 999999}}, 1},
		"dup":              {FnChat, []ChainLink{{Provider: "gemini"}, {Provider: "gemini"}}, 1},
		"retries":          {FnChat, good, 9},
		"mismatched model": {FnChat, []ChainLink{{Provider: "gemini", Model: "claude-sonnet-4-5"}}, 1},
	}
	for name, c := range cases {
		if err := ValidateFunctionConfig(c.fn, c.chain, c.mr); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestGeminiRejectsNonAllowlistedModelBeforeHTTP(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()
	g := NewGeminiProvider("k")
	g.BaseURL = srv.URL
	_, _, err := g.Complete(context.Background(), "../files", "", "x", 0)
	if err == nil || hit {
		t.Fatalf("err=%v hit=%v", err, hit)
	}
}

func TestAPIErrorTrimsBodyInError(t *testing.T) {
	e := &APIError{Status: 400, Body: strings.Repeat("A", 5000) + "SECRET-TAIL"}
	s := e.Error()
	if len(s) > 300 || strings.Contains(s, "SECRET-TAIL") || len(e.Body) != 5011 {
		t.Fatalf("len=%d", len(s))
	}
	if !errors.As(error(e), new(*APIError)) {
		t.Fatal("type lost")
	}
}

func TestRunLogTrimsLongErrors(t *testing.T) {
	logs := captureLogs(t)
	g := &MockProvider{Name: "gemini", Err: errors.New("UNAVAILABLE " + strings.Repeat("x", 4000))}
	a := &MockProvider{Name: "anthropic", Reply: "ok"}
	_, _ = runRecord(regWith(demoChain, FnChat, g, a), FnChat)
	for _, l := range *logs {
		if len(l) > 600 {
			t.Fatalf("log line too long: %d", len(l))
		}
	}
}

func TestRegistrySkipsNonAllowlistedModelLink(t *testing.T) {
	a := &MockProvider{Name: "anthropic", Reply: "ok"}
	g := &MockProvider{Name: "gemini"}
	used, err := runRecord(regWith([]ChainLink{{Provider: "gemini", Model: "evil/../x"}, {Provider: "anthropic"}}, FnChat, g, a), FnChat)
	if err != nil || len(used) != 1 || g.CallCount() != 0 {
		t.Fatalf("used=%v err=%v", used, err)
	}
}

func TestGeminiVisionRequestsJSON(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := new(strings.Builder)
		_, _ = io_copy(b, r)
		body = b.String()
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{}"}]}}]}`))
	}))
	defer srv.Close()
	g := NewGeminiProvider("k")
	g.BaseURL = srv.URL
	if _, _, err := g.Vision(context.Background(), "", []byte{1}, "image/png", "s", "u"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"responseMimeType":"application/json"`) {
		t.Fatalf("body=%s", body)
	}
}

func io_copy(b *strings.Builder, r *http.Request) (int64, error) {
	buf := make([]byte, 1<<16)
	var n int64
	for {
		k, err := r.Body.Read(buf)
		b.Write(buf[:k])
		n += int64(k)
		if err != nil {
			return n, nil
		}
	}
}

func TestEmbeddingAllowlistGeminiAndOpenAI(t *testing.T) {
	ok := [][]ChainLink{
		{{Provider: "openai", Model: "text-embedding-3-small"}},
		{{Provider: "openai"}},
		{{Provider: "gemini", Model: "gemini-embedding-001"}},
	}
	for _, chain := range ok {
		if err := ValidateFunctionConfig(FnEmbedding, chain, 1); err != nil {
			t.Errorf("%+v: %v", chain, err)
		}
	}
}

func TestEmbeddingConfigRejections(t *testing.T) {
	bad := map[string][]ChainLink{
		"two links":             {{Provider: "openai", Model: "text-embedding-3-small"}, {Provider: "gemini", Model: "gemini-embedding-001"}},
		"gemini chat model":     {{Provider: "gemini", Model: "gemini-2.5-flash"}},
		"gemini no model":       {{Provider: "gemini"}},
		"anthropic":             {{Provider: "anthropic"}},
		"cross provider models": {{Provider: "openai", Model: "gemini-embedding-001"}},
	}
	for name, chain := range bad {
		if err := ValidateFunctionConfig(FnEmbedding, chain, 1); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestGeminiEmbeddingModelRejectedForChatFunctions(t *testing.T) {
	for _, fn := range []Function{FnChat, FnIntent, FnOCR, FnPaymentOCR, FnVision, FnMemory} {
		err := ValidateFunctionConfig(fn, []ChainLink{{Provider: "gemini", Model: "gemini-embedding-001"}}, 1)
		if err == nil {
			t.Errorf("%s must reject gemini-embedding-001", fn)
		}
	}
}

func TestGeminiProviderRefusesEmbeddingModelForGenerate(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()
	g := NewGeminiProvider("k")
	g.BaseURL = srv.URL
	if _, _, err := g.Complete(context.Background(), "gemini-embedding-001", "", "x", 0); err == nil || hit {
		t.Fatalf("err=%v hit=%v", err, hit)
	}
}

func TestCatalogListsGeminiEmbeddingAsEmbeddingOnly(t *testing.T) {
	for _, m := range CatalogModels() {
		if m.ID == "gemini-embedding-001" {
			if m.Provider != "gemini" || m.Status != ModelEmbeddingOnly || m.Vision {
				t.Fatalf("unexpected catalog entry: %+v", m)
			}
			return
		}
	}
	t.Fatal("gemini-embedding-001 missing from catalog")
}
