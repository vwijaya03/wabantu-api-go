package aivision

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"encore.app/wabantu/shared/llm"
)

// End-to-end payment_ocr: real GeminiProvider (HTTP fixture) → JSON parse → PaymentProofExtract,
// and Gemini failure → legacy Anthropic vision (HTTP fixture via ANTHROPIC_BASE_URL).

const proofJSON = `{"amount":150000,"bank":"BCA","account_number":"1234567890","account_name":"TOKO DEMO","date":"2026-10-13","confidence":0.93}`

func geminiFixture(t *testing.T, status int, hits *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		if !strings.HasSuffix(r.URL.Path, "/models/gemini-2.5-flash:generateContent") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if status != 200 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"code":503,"status":"UNAVAILABLE"}}`))
			return
		}
		// Gemini may still wrap in fences; sanitizer must handle it.
		txt, _ := jsonQuote("```json\n" + proofJSON + "\n```")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":` + txt + `}]}}],"usageMetadata":{"promptTokenCount":800,"candidatesTokenCount":40}}`))
	}))
}

func anthropicFixture(hits *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		w.Header().Set("Content-Type", "application/json")
		b, _ := jsonQuote(proofJSON)
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[{"type":"text","text":` + b + `}],"stop_reason":"end_turn","usage":{"input_tokens":900,"output_tokens":50}}`))
	}))
}

func setupPaymentOCR(t *testing.T, g *llm.GeminiProvider) {
	st := llm.NewMemoryStore()
	st.Set(llm.FunctionConfig{Function: llm.FnPaymentOCR, Chain: []llm.ChainLink{
		{Provider: "gemini", Model: "gemini-2.5-flash", TimeoutMS: 8000}, {Provider: "anthropic"}}})
	SetVisionResolver(llm.NewResolver(st, 0))
	RegisterVisionProvider(g)
	t.Cleanup(func() {
		SetVisionResolver(llm.NewResolver(nil, 0))
		visionMu.Lock()
		delete(visionExtra, "gemini")
		visionMu.Unlock()
	})
}

func TestPaymentOCRGeminiEndToEnd(t *testing.T) {
	var gh, ah int32
	gs := geminiFixture(t, 200, &gh)
	defer gs.Close()
	as := anthropicFixture(&ah)
	defer as.Close()
	t.Setenv("ANTHROPIC_BASE_URL", as.URL)
	g := llm.NewGeminiProvider("fake")
	g.BaseURL = gs.URL
	setupPaymentOCR(t, g)

	out, u, err := ExtractPaymentProofFromImageForTenant(context.Background(), "anth-fake", "t1", []byte{0xff, 0xd8}, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if out.Amount != 150000 || out.Bank != "BCA" || out.AccountNumber != "1234567890" || out.Confidence < 0.9 {
		t.Fatalf("parsed=%+v", out)
	}
	if u.Provider != "gemini" || u.Model != "gemini-2.5-flash" || u.InputTokens != 800 || gh != 1 || ah != 0 {
		t.Fatalf("usage=%+v gh=%d ah=%d", u, gh, ah)
	}
}

func TestPaymentOCRGeminiDownFallsBackToAnthropic(t *testing.T) {
	var gh, ah int32
	gs := geminiFixture(t, 503, &gh)
	defer gs.Close()
	as := anthropicFixture(&ah)
	defer as.Close()
	t.Setenv("ANTHROPIC_BASE_URL", as.URL)
	g := llm.NewGeminiProvider("fake")
	g.BaseURL = gs.URL
	setupPaymentOCR(t, g)

	out, u, err := ExtractPaymentProofFromImageForTenant(context.Background(), "anth-fake", "t1", []byte{0xff, 0xd8}, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if out.Amount != 150000 || u.Provider != "anthropic" || u.Model != haikuModelID || gh != 1 || ah != 1 {
		t.Fatalf("out=%+v usage=%+v gh=%d ah=%d", out, u, gh, ah)
	}
}

func TestPaymentOCRNoGeminiKeyUsesAnthropic(t *testing.T) {
	var ah int32
	as := anthropicFixture(&ah)
	defer as.Close()
	t.Setenv("ANTHROPIC_BASE_URL", as.URL)
	setupPaymentOCR(t, llm.NewGeminiProvider("")) // registered but no key → skipped
	_, u, err := ExtractPaymentProofFromImageForTenant(context.Background(), "anth-fake", "t1", []byte{1}, "image/png")
	if err != nil || u.Provider != "anthropic" || ah != 1 {
		t.Fatalf("u=%+v err=%v ah=%d", u, err, ah)
	}
}
