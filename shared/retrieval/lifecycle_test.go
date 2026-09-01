package retrieval

import (
	"context"
	"testing"
)

func TestKBVectorIDIncludesModelSlug(t *testing.T) {
	id := KBVectorID("abc", 3, 0)
	want := "kb:abc:mte3s:v3:c0"
	if id != want {
		t.Fatalf("got %q want %q", id, want)
	}
}

func TestKBVectorIDsForVersionsIncludesLegacy(t *testing.T) {
	ids := KBVectorIDsForVersions("e1", EmbeddingModel, 1, 2)
	if len(ids) != 4 {
		t.Fatalf("expected 4 ids, got %d: %v", len(ids), ids)
	}
	if ids[0] != LegacyKBVectorID("e1", 1, 0) {
		t.Fatalf("missing legacy v1: %s", ids[0])
	}
	if ids[1] != KBVectorIDForModel("e1", EmbeddingModel, 1, 0) {
		t.Fatalf("missing model v1: %s", ids[1])
	}
}

func TestKBContentHashChangesWithModel(t *testing.T) {
	h1 := KBContentHash("Q", "A")
	h2 := ContentHash("other-model", "Q", "A")
	if h1 == h2 {
		t.Fatal("hash must differ when model changes")
	}
}

func TestStoredEmbeddingModelOK(t *testing.T) {
	if !StoredEmbeddingModelOK("") {
		t.Fatal("legacy empty model should be ok")
	}
	if !StoredEmbeddingModelOK(EmbeddingModel) {
		t.Fatal("current model should be ok")
	}
	if StoredEmbeddingModelOK("text-embedding-ada-002") {
		t.Fatal("mismatched model must be rejected")
	}
}

func TestPurgeStaleKBVersionsDeletesOlderIDs(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(NewMockEmbedder(), store)
	tenant := TenantIdentity{TenantID: "t1", TenantSchema: "t_test"}
	ns := "t_test"

	legacy := LegacyKBVectorID("entry", 1, 0)
	currentOld := KBVectorID("entry", 2, 0)
	keep := KBVectorID("entry", 3, 0)
	_ = store.Upsert(context.Background(), ns, []VectorRecord{
		{ID: legacy, Values: []float32{0.1}, Metadata: map[string]any{"source": string(SourceKB), "entry_id": "entry", "embedding_model": EmbeddingModel}},
		{ID: currentOld, Values: []float32{0.2}, Metadata: map[string]any{"source": string(SourceKB), "entry_id": "entry", "embedding_model": EmbeddingModel}},
		{ID: keep, Values: []float32{0.3}, Metadata: map[string]any{"source": string(SourceKB), "entry_id": "entry", "embedding_model": EmbeddingModel}},
	})

	if err := PurgeStaleKBVersions(context.Background(), svc, tenant, "entry", 3); err != nil {
		t.Fatalf("purge: %v", err)
	}
	hits, _ := store.Query(context.Background(), ns, []float32{0.3}, 10, nil)
	if len(hits) != 1 || hits[0].ID != keep {
		t.Fatalf("expected only current vector, got %+v", hits)
	}
}

func TestDeleteAllKBEntryVectorsUsesFilter(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(NewMockEmbedder(), store)
	tenant := TenantIdentity{TenantSchema: "t_test"}
	ns := "t_test"
	_ = store.Upsert(context.Background(), ns, []VectorRecord{
		{ID: "orphan", Values: []float32{0.1}, Metadata: map[string]any{
			"source": string(SourceKB), "entry_id": "gone", "embedding_model": EmbeddingModel,
		}},
	})
	if err := DeleteAllKBEntryVectors(context.Background(), svc, tenant, "gone", 1); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	hits, _ := store.Query(context.Background(), ns, []float32{0.1}, 5, nil)
	if len(hits) != 0 {
		t.Fatalf("expected empty namespace, got %v", hits)
	}
}
