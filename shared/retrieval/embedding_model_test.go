package retrieval

import (
	"context"
	"errors"
	"testing"

	"encore.app/wabantu/shared/llm"
)

func withResolver(t *testing.T, fn EmbeddingModelResolver) {
	t.Helper()
	SetEmbeddingModelResolver(fn)
	t.Cleanup(func() { SetEmbeddingModelResolver(nil) })
}

func TestEmbeddingModelFromChain(t *testing.T) {
	cases := []struct {
		name  string
		chain []llm.ChainLink
		want  string
	}{
		{"empty", nil, EmbeddingModel},
		{"openai default", []llm.ChainLink{{Provider: "openai"}}, EmbeddingModel},
		{"openai explicit", []llm.ChainLink{{Provider: "openai", Model: EmbeddingModel}}, EmbeddingModel},
		{"gemini", []llm.ChainLink{{Provider: "gemini", Model: GeminiEmbeddingModel}}, GeminiEmbeddingModel},
		{"unknown model falls back", []llm.ChainLink{{Provider: "gemini", Model: "gemini-2.5-flash"}}, EmbeddingModel},
	}
	for _, c := range cases {
		if got := EmbeddingModelFromChain(c.chain); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestActiveEmbeddingModelUsesLastKnownOnResolverError(t *testing.T) {
	fail := false
	withResolver(t, func(context.Context, string) (string, error) {
		if fail {
			return "", errors.New("db down")
		}
		return GeminiEmbeddingModel, nil
	})
	ctx := context.Background()
	if got := ActiveEmbeddingModel(ctx, "t-lastknown"); got != GeminiEmbeddingModel {
		t.Fatalf("got %q", got)
	}
	fail = true
	if got := ActiveEmbeddingModel(ctx, "t-lastknown"); got != GeminiEmbeddingModel {
		t.Fatalf("expected last known model, got %q", got)
	}
	if got := ActiveEmbeddingModel(ctx, "t-never-seen"); got != EmbeddingModel {
		t.Fatalf("expected default for unseen tenant, got %q", got)
	}
	if _, err := ResolveEmbeddingModel(ctx, "t-lastknown"); err == nil {
		t.Fatal("strict resolve must surface the error")
	}
}

func TestResolveEmbeddingModelRejectsNonEmbeddingValue(t *testing.T) {
	withResolver(t, func(context.Context, string) (string, error) { return "claude-haiku-4-5", nil })
	m, err := ResolveEmbeddingModel(context.Background(), "t1")
	if err != nil || m != EmbeddingModel {
		t.Fatalf("m=%q err=%v", m, err)
	}
}

func TestServiceEmbedderForPicksPerTenantModel(t *testing.T) {
	openai, gem := NewMockEmbedder(), NewMockEmbedder()
	svc := NewService(openai, NewMemoryStore())
	svc.Embedders = map[string]Embedder{EmbeddingModel: openai, GeminiEmbeddingModel: gem}
	withResolver(t, func(_ context.Context, tenantID string) (string, error) {
		if tenantID == "gem" {
			return GeminiEmbeddingModel, nil
		}
		return EmbeddingModel, nil
	})
	ctx := context.Background()
	if e, m, err := svc.embedderFor(ctx, "gem"); err != nil || m != GeminiEmbeddingModel || e != Embedder(gem) {
		t.Fatalf("gemini tenant: m=%q err=%v", m, err)
	}
	if e, m, err := svc.embedderFor(ctx, "other"); err != nil || m != EmbeddingModel || e != Embedder(openai) {
		t.Fatalf("default tenant: m=%q err=%v", m, err)
	}
}

func TestServiceEmbedderForMissingCredentialsAndResolverError(t *testing.T) {
	openai := NewMockEmbedder()
	svc := NewService(openai, NewMemoryStore())
	svc.Embedders = map[string]Embedder{EmbeddingModel: openai}
	withResolver(t, func(_ context.Context, tenantID string) (string, error) {
		if tenantID == "err" {
			return "", errors.New("db down")
		}
		return GeminiEmbeddingModel, nil
	})
	_, _, err := svc.embedderFor(context.Background(), "t1")
	if !errors.Is(err, ErrServiceNotConfigured) {
		t.Fatalf("missing key must wrap ErrServiceNotConfigured, got %v", err)
	}
	if _, _, err := svc.embedderFor(context.Background(), "err"); err == nil {
		t.Fatal("resolver error must not be swallowed")
	}
}

func TestServiceLegacySingleEmbedderIgnoresResolver(t *testing.T) {
	called := false
	withResolver(t, func(context.Context, string) (string, error) { called = true; return GeminiEmbeddingModel, nil })
	svc := NewService(NewMockEmbedder(), NewMemoryStore())
	_, m, err := svc.embedderFor(context.Background(), "t1")
	if err != nil || m != EmbeddingModel || called {
		t.Fatalf("m=%q err=%v resolverCalled=%v", m, err, called)
	}
}

func TestStoredEmbeddingModelOKForActiveModel(t *testing.T) {
	if !StoredEmbeddingModelOKFor("", GeminiEmbeddingModel) {
		t.Fatal("legacy empty must pass")
	}
	if !StoredEmbeddingModelOKFor(GeminiEmbeddingModel, GeminiEmbeddingModel) {
		t.Fatal("matching gemini must pass")
	}
	if StoredEmbeddingModelOKFor(EmbeddingModel, GeminiEmbeddingModel) {
		t.Fatal("openai row must not pass for gemini tenant")
	}
	if StoredEmbeddingModelOKFor(GeminiEmbeddingModel, EmbeddingModel) {
		t.Fatal("gemini row must not pass for openai tenant")
	}
}

func TestModelScopedVectorIDsAndFilters(t *testing.T) {
	if KBVectorIDForModel("e", GeminiEmbeddingModel, 1, 0) == KBVectorIDForModel("e", EmbeddingModel, 1, 0) {
		t.Fatal("vector ids must differ per model")
	}
	ch := ChunkKBForModel(GeminiEmbeddingModel, "e1", 2, "q", "a")
	if len(ch) != 1 || ch[0].ID != KBVectorIDForModel("e1", GeminiEmbeddingModel, 2, 0) {
		t.Fatalf("chunk id: %+v", ch)
	}
	f := PineconeFilterActiveKBForModel(GeminiEmbeddingModel)
	if f["embedding_model"].(map[string]any)["$eq"] != GeminiEmbeddingModel {
		t.Fatalf("filter: %+v", f)
	}
	if len(allModelKBIDs("e", 1, 1)) != 2*len(KnownEmbeddingModels()) {
		t.Fatal("delete ids must cover legacy + every known model")
	}
}

func TestIndexKBStampsActiveModelMetadata(t *testing.T) {
	store := NewMemoryStore()
	emb := NewMockEmbedder()
	svc := NewService(emb, store)
	svc.Embedders = map[string]Embedder{GeminiEmbeddingModel: emb}
	withResolver(t, func(context.Context, string) (string, error) { return GeminiEmbeddingModel, nil })

	tenant := TenantIdentity{TenantID: "t1", TenantSchema: "t_demo"}
	if err := svc.IndexKB(context.Background(), tenant, "e1", "cat", "Q?", "A.", 1); err != nil {
		t.Fatal(err)
	}
	hits, err := store.Query(context.Background(), "t_demo", mustEmbed(t, emb, "Q?\n\nA."), 3, PineconeFilterActiveKBForModel(GeminiEmbeddingModel))
	if err != nil || len(hits) != 1 {
		t.Fatalf("expected 1 gemini hit, got %d err=%v", len(hits), err)
	}
	other, _ := store.Query(context.Background(), "t_demo", mustEmbed(t, emb, "Q?\n\nA."), 3, PineconeFilterActiveKBForModel(EmbeddingModel))
	if len(other) != 0 {
		t.Fatalf("openai-filtered query must not see gemini vectors, got %d", len(other))
	}
}

func mustEmbed(t *testing.T, e Embedder, text string) []float32 {
	t.Helper()
	v, err := e.Embed(context.Background(), []string{text})
	if err != nil || len(v) != 1 {
		t.Fatalf("embed: %v", err)
	}
	return v[0]
}
