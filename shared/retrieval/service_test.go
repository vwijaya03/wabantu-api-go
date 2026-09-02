package retrieval

import (
	"context"
	"errors"
	"testing"
	"time"
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
func (noopStore) DeleteNamespace(context.Context, string) error { return nil }

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
	svc.Breaker = NewCircuitBreaker(1, time.Hour)
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
	if res.FallbackReason != FallbackReasonCircuitOpen {
		t.Fatalf("expected circuit_open reason, got %q", res.FallbackReason)
	}
}

func TestRetrieveKB_ClientTimeoutDoesNotTripBreaker(t *testing.T) {
	svc := NewService(ctxDeadlineEmbedder{}, noopStore{})
	svc.Breakers = NewBreakerPool(1, time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Nanosecond)

	res, err := svc.RetrieveKB(ctx, RetrieveKBRequest{
		Tenant: TenantIdentity{TenantID: "t_timeout", TenantSchema: "t_acme"},
		Query:  "harga",
		Mode:   ModeVector,
	}, func(_ context.Context, _ string, _ int) ([]ScoredEntry, error) {
		return []ScoredEntry{{EntryID: "e1", Score: 1}}, nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res == nil || !res.LexicalFallback {
		t.Fatalf("expected lexical fallback, got %+v", res)
	}
	if res.FallbackReason != FallbackReasonClientTimeout {
		t.Fatalf("expected client_timeout, got %q", res.FallbackReason)
	}
	cb := svc.Breakers.For("t_timeout")
	if cb.Open() {
		t.Fatal("caller canceled should not open per-tenant breaker")
	}
}

func TestRetrieveKB_BudgetExceededTripsBreakerWhenParentAlive(t *testing.T) {
	svc := NewService(ctxDeadlineEmbedder{}, noopStore{})
	svc.Breakers = NewBreakerPool(1, time.Hour)
	parent := context.Background()
	child, cancel := context.WithTimeout(parent, 1*time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Nanosecond)

	res, err := svc.RetrieveKB(child, RetrieveKBRequest{
		Tenant:    TenantIdentity{TenantID: "t_budget", TenantSchema: "t_acme"},
		Query:     "harga",
		Mode:      ModeVector,
		ParentCtx: parent,
	}, func(_ context.Context, _ string, _ int) ([]ScoredEntry, error) {
		return []ScoredEntry{{EntryID: "e1", Score: 1}}, nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res == nil || !res.LexicalFallback {
		t.Fatalf("expected lexical fallback, got %+v", res)
	}
	cb := svc.Breakers.For("t_budget")
	if !cb.Open() {
		t.Fatal("budget_exceeded should open per-tenant breaker when parent alive")
	}
}

type ctxDeadlineEmbedder struct{}

func (ctxDeadlineEmbedder) Embed(ctx context.Context, _ []string) ([][]float32, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (ctxDeadlineEmbedder) Dimensions() int { return 1536 }
func (ctxDeadlineEmbedder) Model() string   { return "test" }

func TestRetrieveKB_ZeroVectorHitsFlag(t *testing.T) {
	store := mockVectorStore{hits: []Hit{}}
	svc := NewService(NewMockEmbedder(), store)
	res, err := svc.RetrieveKB(context.Background(), RetrieveKBRequest{
		Tenant: TenantIdentity{TenantID: "t1", TenantSchema: "t_acme"},
		Query:  "harga",
		Mode:   ModeVector,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.ZeroVectorHits {
		t.Fatalf("expected ZeroVectorHits=true, got %+v", res)
	}
}

type mockVectorStore struct {
	hits []Hit
}

func (m mockVectorStore) Upsert(context.Context, string, []VectorRecord) error { return nil }
func (m mockVectorStore) Query(context.Context, string, []float32, int, map[string]any) ([]Hit, error) {
	return m.hits, nil
}
func (m mockVectorStore) DeleteIDs(context.Context, string, []string) error { return nil }
func (m mockVectorStore) DeleteByFilter(context.Context, string, map[string]any) error {
	return nil
}
func (m mockVectorStore) DeleteNamespace(context.Context, string) error { return nil }

func TestRetrieveKB_VectorFloorFiltersNoise(t *testing.T) {
	store := mockVectorStore{hits: []Hit{
		{ID: "kb:good:v1:c0", Score: 0.45, Metadata: map[string]any{"entry_id": "good"}},
		{ID: "kb:noise:v1:c0", Score: 0.05, Metadata: map[string]any{"entry_id": "noise"}},
	}}
	svc := NewService(NewMockEmbedder(), store)
	res, err := svc.RetrieveKB(context.Background(), RetrieveKBRequest{
		Tenant: TenantIdentity{TenantID: "t1", TenantSchema: "t_acme"},
		Query:  "harga produk",
		TopK:   5,
		Mode:   ModeVector,
	}, func(_ context.Context, _ string, _ int) ([]ScoredEntry, error) {
		return []ScoredEntry{{EntryID: "good", Score: 0.2, Source: SourceKB}}, nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(res.VectorHits) != 1 || res.VectorHits[0].Metadata["entry_id"] != "good" {
		t.Fatalf("expected noise filtered from vector hits, got %+v", res.VectorHits)
	}
	foundNoise := false
	for _, e := range res.Entries {
		if e.EntryID == "noise" {
			foundNoise = true
		}
	}
	if foundNoise {
		t.Fatalf("noise entry should not appear in fused results: %+v", res.Entries)
	}
}
