package retrieval

import (
	"context"
	"errors"
	"testing"
)

type failingEmbedder struct{}

func (failingEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("embed 503 unavailable")
}
func (failingEmbedder) Dimensions() int { return 1536 }
func (failingEmbedder) Model() string   { return "test" }

type noopStore struct{}

func (noopStore) Upsert(context.Context, string, []VectorRecord) error { return nil }
func (noopStore) Query(context.Context, string, []float32, int, map[string]any) ([]Hit, error) {
	return nil, nil
}
func (noopStore) DeleteIDs(context.Context, string, []string) error { return nil }
func (noopStore) DeleteByFilter(context.Context, string, map[string]any) error {
	return nil
}

func TestRetrieveKB_VectorFailureSetsLexicalFallback(t *testing.T) {
	svc := NewService(failingEmbedder{}, noopStore{})
	res, err := svc.RetrieveKB(context.Background(), RetrieveKBRequest{
		Tenant: TenantIdentity{TenantID: "t1", TenantSchema: "t_acme"},
		Query:  "harga",
		TopK:   5,
		Mode:   ModeVector,
	}, func(_ context.Context, _ string, topK int) ([]ScoredEntry, error) {
		return []ScoredEntry{{EntryID: "e1", Score: 1, Source: SourceKB}}, nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res == nil || !res.LexicalFallback {
		t.Fatalf("expected LexicalFallback=true, got %+v", res)
	}
	if len(res.Entries) != 1 || res.Entries[0].EntryID != "e1" {
		t.Fatalf("expected lexical entry, got %+v", res.Entries)
	}
}

func TestRetrieveKB_CircuitOpenSetsLexicalFallback(t *testing.T) {
	svc := NewService(NewMockEmbedder(), noopStore{})
	svc.Breaker = NewCircuitBreaker(1, 0)
	svc.Breaker.RecordFailure(errors.New("pinecone down"))

	res, err := svc.RetrieveKB(context.Background(), RetrieveKBRequest{
		Tenant: TenantIdentity{TenantID: "t1", TenantSchema: "t_acme"},
		Query:  "harga",
		Mode:   ModeVector,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res == nil || !res.LexicalFallback {
		t.Fatalf("expected LexicalFallback on circuit open, got %+v", res)
	}
}
