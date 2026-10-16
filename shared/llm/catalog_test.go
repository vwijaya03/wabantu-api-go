package llm

import "testing"

func TestCatalogModels(t *testing.T) {
	ms := CatalogModels()
	var sawOpenAI, sawFlash bool
	for _, m := range ms {
		if m.Provider == "openai" {
			sawOpenAI = true
			if m.Status != ModelEmbeddingOnly || m.Vision {
				t.Errorf("openai must be embedding_only: %+v", m)
			}
		}
		if m.ID == "gemini-2.5-flash" {
			sawFlash = m.Status == ModelSupported && m.Vision && m.Label != ""
		}
	}
	if !sawOpenAI || !sawFlash {
		t.Fatalf("catalog=%+v", ms)
	}
}

func TestOpenAINotAllowedForChat(t *testing.T) {
	if err := ValidateFunctionConfig(FnChat, []ChainLink{{Provider: "openai", Model: "text-embedding-3-small"}}, 0); err == nil {
		t.Fatal("openai must be rejected for chat")
	}
}

func TestAvailabilityAndInvalidateHooks(t *testing.T) {
	SetProviderAvailable("gemini", true)
	if !ProviderAvailable("gemini") || ProviderAvailable("nope") {
		t.Fatal("availability")
	}
	n := 0
	OnConfigChanged(func() { n++ })
	NotifyConfigChanged()
	if n != 1 {
		t.Fatal("hook not called")
	}
}

func TestCatalogGeminiLatestChatModels(t *testing.T) {
	want := map[string]string{
		"gemini-3.8-flash":       "Gemini 3.8 Flash",
		"gemini-3.6-flash":       "Gemini 3.6 Flash",
		"gemini-3.5-flash-lite":  "Gemini 3.5 Flash-Lite",
		"gemini-3.1-pro-preview": "Gemini 3.1 Pro (Preview)",
	}
	got := map[string]ModelInfo{}
	for _, m := range CatalogModels() {
		if m.Provider == "gemini" {
			got[m.ID] = m
		}
	}
	for id, label := range want {
		m, ok := got[id]
		if !ok {
			t.Errorf("%s missing from gemini catalog", id)
			continue
		}
		if m.Label != label || m.Status != ModelSupported || !m.Vision {
			t.Errorf("%s: unexpected %+v", id, m)
		}
		if err := ValidateFunctionConfig(FnChat, []ChainLink{{Provider: "gemini", Model: id}}, 1); err != nil {
			t.Errorf("%s must be valid for chat: %v", id, err)
		}
	}
}
