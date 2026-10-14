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
