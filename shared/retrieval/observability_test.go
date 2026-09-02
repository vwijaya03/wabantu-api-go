package retrieval

import "testing"

func TestSnapshotObservabilityRatios(t *testing.T) {
	ResetObservabilityForTest()
	origBudget := secrets.RetrievalBudgetMs
	secrets.RetrievalBudgetMs = "1200"
	defer func() { secrets.RetrievalBudgetMs = origBudget }()

	retrievalRequests.Add(1)
	retrievalFallback.Add(1)
	retrievalZeroHit.Add(1)
	retrievalRequests.Add(1)

	snap := SnapshotObservability()
	if snap.Requests != 2 {
		t.Fatalf("requests=%d", snap.Requests)
	}
	if snap.Fallbacks != 1 || snap.ZeroHits != 1 {
		t.Fatalf("fallbacks=%d zero=%d", snap.Fallbacks, snap.ZeroHits)
	}
	if snap.FallbackRatio != 0.5 || snap.ZeroHitRatio != 0.5 {
		t.Fatalf("ratios fb=%v zh=%v", snap.FallbackRatio, snap.ZeroHitRatio)
	}

	RecordQueryLatency(50)
	RecordQueryLatency(150)
	RecordEmbedLatency(100)
	RecordStoreLatency(40)
	RecordErrorCategory(CategoryBudgetExceeded, ProviderOpenAI)

	snap = SnapshotObservability()
	if snap.LatencyP50Ms == 0 || snap.LatencyP95Ms == 0 {
		t.Fatalf("latency p50=%d p95=%d", snap.LatencyP50Ms, snap.LatencyP95Ms)
	}
	if snap.EmbedLatencyP95Ms == 0 || snap.StoreLatencyP95Ms == 0 {
		t.Fatalf("embed/store p95 missing")
	}
	if snap.BudgetMs == 0 {
		t.Fatal("budgetMs should be set")
	}
	if snap.Status != "insufficient_data" {
		t.Fatalf("expected insufficient_data with few samples, got %q", snap.Status)
	}
	if snap.ErrorsByCategory["budget_exceeded:openai"] != 1 {
		t.Fatalf("errorsByCategory=%v", snap.ErrorsByCategory)
	}
}

func TestObservabilityStatusCriticalOnHighFallback(t *testing.T) {
	ResetObservabilityForTest()
	origBudget := secrets.RetrievalBudgetMs
	secrets.RetrievalBudgetMs = "1200"
	defer func() { secrets.RetrievalBudgetMs = origBudget }()
	for i := 0; i < 25; i++ {
		retrievalRequests.Add(1)
		retrievalFallback.Add(1)
		RecordQueryLatency(100)
	}
	snap := SnapshotObservability()
	if snap.Status != "critical" {
		t.Fatalf("expected critical, got %q", snap.Status)
	}
}
