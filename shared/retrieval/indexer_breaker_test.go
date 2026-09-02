package retrieval

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetrieveCatalogCandidates_BreakerOpenFailFast(t *testing.T) {
	t.Parallel()
	svc := NewService(NewMockEmbedder(), noopStore{})
	svc.Breakers = NewBreakerPool(1, time.Hour)
	cb := svc.Breakers.For("tenant-catalog")
	cb.RecordFailure(errors.New("provider down"))

	_, err := svc.RetrieveCatalogCandidates(context.Background(), context.Background(), TenantIdentity{
		TenantID: "tenant-catalog", TenantSchema: "t_acme",
	}, "harga", 3)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestRetrieveCatalogCandidates_RecordsLatency(t *testing.T) {
	t.Parallel()
	ResetObservabilityForTest()
	origBudget := secrets.RetrievalBudgetMs
	secrets.RetrievalBudgetMs = "1200"
	defer func() { secrets.RetrievalBudgetMs = origBudget }()
	store := mockVectorStore{hits: []Hit{{ID: "c1", Score: 0.9}}}
	svc := NewService(NewMockEmbedder(), store)
	_, err := svc.RetrieveCatalogCandidates(context.Background(), context.Background(), TenantIdentity{
		TenantID: "t1", TenantSchema: "t_acme",
	}, "celana", 3)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	snap := SnapshotObservability()
	if snap.LatencyP95Ms == 0 {
		t.Fatal("expected query latency recorded")
	}
}
