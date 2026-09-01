package retrieval

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"encore.dev/rlog"
)

var (
	retrievalRequests atomic.Uint64
	retrievalFallback atomic.Uint64
	retrievalZeroHit  atomic.Uint64
	embedCacheHits    atomic.Uint64
	embedCacheMisses  atomic.Uint64
	indexingSuccess      atomic.Uint64
	indexingFailure      atomic.Uint64
	embedQuotaRejected   atomic.Uint64
)

type latencyTracker struct {
	mu       sync.Mutex
	samples  []uint64
	capacity int
}

var queryLatency = &latencyTracker{capacity: 2048}

func (t *latencyTracker) record(ms uint64) {
	if t == nil || ms == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.samples) >= t.capacity {
		t.samples = t.samples[1:]
	}
	t.samples = append(t.samples, ms)
}

func (t *latencyTracker) percentile(p float64) uint64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.samples) == 0 {
		return 0
	}
	cp := append([]uint64(nil), t.samples...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := int(float64(len(cp)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

// LogQuery records retrieval observability fields (structured logs + in-process counters).
func LogQuery(ctx context.Context, source, tenantID, mode string, failed, zeroResult bool) {
	retrievalRequests.Add(1)
	if failed {
		retrievalFallback.Add(1)
	}
	if zeroResult {
		retrievalZeroHit.Add(1)
	}
	rlog.Info("retrieval query",
		"source", source,
		"tenant_id", tenantID,
		"retrieval_mode", mode,
		"fallback", failed,
		"zero_result", zeroResult,
	)
	_ = ctx
}

// RecordQueryLatency tracks retrieval latency for percentile gauges.
func RecordQueryLatency(d time.Duration) {
	ms := uint64(d.Milliseconds())
	if ms == 0 && d > 0 {
		ms = 1
	}
	queryLatency.record(ms)
}

// LatencyP95Ms returns the in-process p95 retrieval latency in milliseconds.
func LatencyP95Ms() uint64 {
	return queryLatency.percentile(0.95)
}

// RecordIndexingOutcome records indexing worker success/failure in-process counters.
func RecordIndexingOutcome(entity, lane string, success bool, lag time.Duration) {
	if success {
		indexingSuccess.Add(1)
	} else {
		indexingFailure.Add(1)
	}
	_ = entity
	_ = lane
	_ = lag
}

// RecordEmbedQuotaRejected increments when a tenant exceeds hourly embed quota.
func RecordEmbedQuotaRejected() {
	embedQuotaRejected.Add(1)
}

// EmbedQuotaRejected returns the in-process embed quota rejection counter.
func EmbedQuotaRejected() uint64 {
	return embedQuotaRejected.Load()
}

// RecordEmbedCacheStats increments in-process cache counters (also called from CachingEmbedder).
func RecordEmbedCacheStats(hits, misses uint64) {
	if hits > 0 {
		embedCacheHits.Add(hits)
	}
	if misses > 0 {
		embedCacheMisses.Add(misses)
	}
}

// ObservabilitySnapshot is returned by the superadmin metrics API.
type ObservabilitySnapshot struct {
	Requests        uint64 `json:"requests"`
	Fallbacks       uint64 `json:"fallbacks"`
	ZeroHits        uint64 `json:"zeroHits"`
	FallbackRatio   float64 `json:"fallbackRatio"`
	ZeroHitRatio    float64 `json:"zeroHitRatio"`
	LatencyP50Ms    uint64 `json:"latencyP50Ms"`
	LatencyP95Ms    uint64 `json:"latencyP95Ms"`
	LatencyP99Ms    uint64 `json:"latencyP99Ms"`
	EmbedCacheHits  uint64 `json:"embedCacheHits"`
	EmbedCacheMisses uint64 `json:"embedCacheMisses"`
	EmbedCacheHitRatio float64 `json:"embedCacheHitRatio"`
	IndexingSuccess uint64 `json:"indexingSuccess"`
	IndexingFailure uint64 `json:"indexingFailure"`
}

// SnapshotObservability returns in-process counters and latency percentiles.
func SnapshotObservability() ObservabilitySnapshot {
	req := retrievalRequests.Load()
	fb := retrievalFallback.Load()
	zh := retrievalZeroHit.Load()
	ch := embedCacheHits.Load()
	cm := embedCacheMisses.Load()
	snap := ObservabilitySnapshot{
		Requests:         req,
		Fallbacks:        fb,
		ZeroHits:         zh,
		LatencyP50Ms:     queryLatency.percentile(0.50),
		LatencyP95Ms:     queryLatency.percentile(0.95),
		LatencyP99Ms:     queryLatency.percentile(0.99),
		EmbedCacheHits:   ch,
		EmbedCacheMisses: cm,
		IndexingSuccess:  indexingSuccess.Load(),
		IndexingFailure:  indexingFailure.Load(),
	}
	if req > 0 {
		snap.FallbackRatio = float64(fb) / float64(req)
		snap.ZeroHitRatio = float64(zh) / float64(req)
	}
	cacheTotal := ch + cm
	if cacheTotal > 0 {
		snap.EmbedCacheHitRatio = float64(ch) / float64(cacheTotal)
	}
	return snap
}

// Stats returns in-process counters (tests / debug).
func Stats() (requests, fallbacks, zeroHits uint64) {
	return retrievalRequests.Load(), retrievalFallback.Load(), retrievalZeroHit.Load()
}
