package retrieval

import "testing"

func TestReciprocalRankFusion(t *testing.T) {
	lists := []RankedList{
		{Source: SourceKB, Items: []ScoredEntry{
			{EntryID: "a", Score: 0.9},
			{EntryID: "b", Score: 0.8},
		}},
		{Source: SourceKB, Items: []ScoredEntry{
			{EntryID: "b", Score: 0.85},
			{EntryID: "c", Score: 0.7},
		}},
	}
	fused := ReciprocalRankFusion(lists, 60)
	if len(fused) != 3 {
		t.Fatalf("expected 3 fused entries, got %d", len(fused))
	}
	if fused[0].EntryID != "b" {
		t.Fatalf("expected b first, got %s", fused[0].EntryID)
	}
}

func TestMockEmbedderDimensions(t *testing.T) {
	m := NewMockEmbedder()
	if m.Dimensions() != EmbeddingDims {
		t.Fatalf("expected %d dims, got %d", EmbeddingDims, m.Dimensions())
	}
	vecs, err := m.Embed(t.Context(), []string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || len(vecs[0]) != EmbeddingDims {
		t.Fatalf("unexpected vector shape: %+v", vecs)
	}
	if vecs[0][0] == vecs[1][0] {
		t.Fatal("expected different vectors for different texts")
	}
}

func TestMemoryStoreQuery(t *testing.T) {
	store := NewMemoryStore()
	emb := NewMockEmbedder()
	vecs, _ := emb.Embed(t.Context(), []string{"berapa harga produk"})
	rec := VectorRecord{ID: "kb:1:v1:c0", Values: vecs[0], Metadata: map[string]any{
		"source": "kb", "entry_id": "1",
	}}
	if err := store.Upsert(t.Context(), "t_test", []VectorRecord{rec}); err != nil {
		t.Fatal(err)
	}
	hits, err := store.Query(t.Context(), "t_test", vecs[0], 5, PineconeFilterActiveKB())
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].ID != "kb:1:v1:c0" {
		t.Fatalf("expected hit, got %+v", hits)
	}
}

func TestCircuitBreakerOpens(t *testing.T) {
	cb := NewCircuitBreaker(2, 0)
	if !cb.Allow() {
		t.Fatal("expected allow initially")
	}
	cb.RecordFailure(fmtErr("fail"))
	cb.RecordFailure(fmtErr("fail"))
	if cb.Allow() {
		t.Fatal("expected open after threshold")
	}
}

type fmtErr string

func (e fmtErr) Error() string { return string(e) }
