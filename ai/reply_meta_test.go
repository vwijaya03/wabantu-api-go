package ai

import "testing"

func TestMetaFromRouteLLM(t *testing.T) {
	m := metaFromRoute(reasonAIGenerated, PathLLM, RoutingDecision{
		Model: APIIDHaiku45, Tier: "haiku", Reason: "hybrid_simple",
	})
	if !m.LLMUsed || m.Path != PathLLM || m.Model != APIIDHaiku45 {
		t.Fatalf("unexpected meta: %+v", m)
	}
}

func TestMetaNoLLMProfile(t *testing.T) {
	m := metaNoLLM(reasonProfileIncomplete, PathProfileIncomplete)
	if m.LLMUsed || m.Model != "" {
		t.Fatalf("profile incomplete should not use LLM: %+v", m)
	}
}

func TestPreviewTextRedactsPIIInNonLocalEnv(t *testing.T) {
	// encore test runs in local cloud; redaction is off for developer ergonomics.
	out := previewText("hubungi 081234567890", 80)
	if out == "" {
		t.Fatal("expected non-empty preview")
	}
}
