package retrieval

import "testing"

func TestSnapshotObservabilityRatios(t *testing.T) {
	retrievalRequests.Store(0)
	retrievalFallback.Store(0)
	retrievalZeroHit.Store(0)
	embedCacheHits.Store(0)
	embedCacheMisses.Store(0)

	LogQuery(nil, "kb", "t1", "vector", true, true)
	LogQuery(nil, "kb", "t1", "vector", false, false)

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
	snap = SnapshotObservability()
	if snap.LatencyP50Ms == 0 || snap.LatencyP95Ms == 0 {
		t.Fatalf("latency p50=%d p95=%d", snap.LatencyP50Ms, snap.LatencyP95Ms)
	}
}
