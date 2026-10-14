package aivision

import (
	"context"
	"strings"
	"testing"

	"encore.app/wabantu/shared/llm"
)

func TestVisionRegistryDefaultIsLegacyAnthropic(t *testing.T) {
	_, _, err := ExtractPaymentProofFromImage(context.Background(), "", []byte{1}, "image/png")
	if err == nil || !strings.Contains(err.Error(), "kunci Anthropic belum dikonfigurasi") {
		t.Fatalf("want legacy anthropic error, got %v", err)
	}
}

func TestVisionRegistryRoutesOCRToConfiguredProvider(t *testing.T) {
	st := llm.NewMemoryStore()
	st.Set(llm.FunctionConfig{Function: llm.FnPaymentOCR, Chain: []llm.ChainLink{{Provider: "gemini", Model: "gemini-2.5-flash"}}})
	SetVisionResolver(llm.NewResolver(st, 0))
	m := &llm.MockProvider{Name: "gemini", Reply: "```json\n{\"amount\":15000}\n```"}
	RegisterVisionProvider(m)
	t.Cleanup(func() {
		SetVisionResolver(llm.NewResolver(nil, 0))
		visionMu.Lock()
		delete(visionExtra, "gemini")
		visionMu.Unlock()
	})

	if _, _, err := ExtractPaymentProofFromImage(context.Background(), "", []byte{1}, "image/png"); err != nil {
		t.Fatalf("ocr via mock: %v", err)
	}
	if m.Calls != 1 {
		t.Fatalf("mock calls=%d", m.Calls)
	}
	// FnVision has no row → still Anthropic default (fails on empty key, mock untouched).
	if _, _, err := ExtractProductMatchFromImage(context.Background(), "", []byte{1}, "image/png"); err == nil || m.Calls != 1 {
		t.Fatalf("vision should stay on anthropic: err=%v calls=%d", err, m.Calls)
	}
}

func TestPaymentOCRFallsBackToAnthropicOnGeminiRetryable(t *testing.T) {
	st := llm.NewMemoryStore()
	st.Set(llm.FunctionConfig{Function: llm.FnPaymentOCR, TenantID: "t9", Chain: []llm.ChainLink{{Provider: "gemini", Model: "gemini-2.5-flash"}, {Provider: "anthropic"}}})
	SetVisionResolver(llm.NewResolver(st, 0))
	m := &llm.MockProvider{Name: "gemini", Err: &llm.APIError{Status: 503, Body: "UNAVAILABLE"}}
	RegisterVisionProvider(m)
	t.Cleanup(func() {
		SetVisionResolver(llm.NewResolver(nil, 0))
		visionMu.Lock()
		delete(visionExtra, "gemini")
		visionMu.Unlock()
	})
	_, _, err := ExtractPaymentProofFromImageForTenant(context.Background(), "", "t9", []byte{1}, "image/png")
	if m.Calls != 1 || err == nil || !strings.Contains(err.Error(), "kunci Anthropic") {
		t.Fatalf("want gemini tried then anthropic legacy path: calls=%d err=%v", m.Calls, err)
	}
	// Other tenant: no row → Anthropic only, gemini untouched.
	_, _, _ = ExtractPaymentProofFromImageForTenant(context.Background(), "", "other", []byte{1}, "image/png")
	if m.Calls != 1 {
		t.Fatalf("other tenant must not hit gemini, calls=%d", m.Calls)
	}
}

func TestPaymentOCRUsageReportsModel(t *testing.T) {
	st := llm.NewMemoryStore()
	st.Set(llm.FunctionConfig{Function: llm.FnPaymentOCR, Chain: []llm.ChainLink{{Provider: "gemini", Model: "gemini-2.5-flash"}}})
	SetVisionResolver(llm.NewResolver(st, 0))
	RegisterVisionProvider(&llm.MockProvider{Name: "gemini", Reply: `{"amount":1}`})
	t.Cleanup(func() {
		SetVisionResolver(llm.NewResolver(nil, 0))
		visionMu.Lock()
		delete(visionExtra, "gemini")
		visionMu.Unlock()
	})
	_, u, err := ExtractPaymentProofFromImageForTenant(context.Background(), "", "t1", []byte{1}, "image/png")
	if err != nil || u.Provider != "gemini" || u.Model != "gemini-2.5-flash" {
		t.Fatalf("%+v %v", u, err)
	}
}
