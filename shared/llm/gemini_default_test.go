package llm

import "testing"

func TestGeminiDefaultModelAllowlisted(t *testing.T) {
	found := false
	for _, id := range ModelAllowlist["gemini"] {
		if id == GeminiDefaultModel {
			found = true
		}
	}
	if !found {
		t.Fatalf("Gemini default %q is not in ModelAllowlist[gemini]", GeminiDefaultModel)
	}
	if got := NewGeminiProvider("x").DefaultModel; got != GeminiDefaultModel {
		t.Fatalf("provider default = %q, want %q", got, GeminiDefaultModel)
	}
	if _, ok := modelMeta[GeminiDefaultModel]; !ok {
		t.Fatalf("Gemini default %q missing catalog label", GeminiDefaultModel)
	}
}
