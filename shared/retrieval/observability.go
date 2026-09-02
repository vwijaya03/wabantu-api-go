package retrieval

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"encore.dev/rlog"
)

const observabilityMinSamples = 20

var (
	retrievalRequests atomic.Uint64
	retrievalFallback atomic.Uint64
	retrievalZeroHit  atomic.Uint64
	embedCacheHits    atomic.Uint64
	embedCacheMisses  atomic.Uint64
	indexingSuccess   atomic.Uint64
	indexingFailure   atomic.Uint64
	indexingDLQ       atomic.Uint64
	embedQuotaRejected atomic.Uint64
)

type latencyTracker struct {
	mu       sync.Mutex
	samples  []uint64
	capacity int
}

var (
	queryLatency = &latencyTracker{capacity: 2048}
	embedLatency = &latencyTracker{capacity: 2048}
	storeLatency = &latencyTracker{capacity: 2048}
)

type errorCounterKey struct {
	category ErrorCategory
	provider Provider
}

var (
	errorCounterMu sync.Mutex
	errorCounters  = map[errorCounterKey]uint64{}
)

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

func (t *latencyTracker) count() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.samples)
}

// LogQuery records retrieval observability fields (structured logs + in-process counters).
func LogQuery(ctx context.Context, source, tenantID, mode string, failed, zeroResult bool) {
	LogQueryWithReason(ctx, source, tenantID, mode, failed, zeroResult, "")
}

// LogQueryWithReason adds optional fallback_reason for ops triage.
func LogQueryWithReason(ctx context.Context, source, tenantID, mode string, failed, zeroResult bool, fallbackReason FallbackReason) {
	retrievalRequests.Add(1)
	if failed {
		retrievalFallback.Add(1)
	}
	if zeroResult {
		retrievalZeroHit.Add(1)
	}
	fields := []any{
		"source", source,
		"tenant_id", tenantID,
		"retrieval_mode", mode,
		"fallback", failed,
		"zero_result", zeroResult,
	}
	if fallbackReason != "" {
		fields = append(fields, "fallback_reason", string(fallbackReason))
	}
	rlog.Info("retrieval query", fields...)
	_ = ctx
}

// RecordQueryLatency tracks end-to-end retrieval latency for percentile gauges.
func RecordQueryLatency(d time.Duration) {
	ms := durationToMs(d)
	queryLatency.record(ms)
}

// RecordEmbedLatency tracks OpenAI embed step latency.
func RecordEmbedLatency(d time.Duration) {
	embedLatency.record(durationToMs(d))
}

// RecordStoreLatency tracks Pinecone query step latency.
func RecordStoreLatency(d time.Duration) {
	storeLatency.record(durationToMs(d))
}

func durationToMs(d time.Duration) uint64 {
	ms := uint64(d.Milliseconds())
	if ms == 0 && d > 0 {
		ms = 1
	}
	return ms
}

// RecordErrorCategory increments per-category/provider counters.
func RecordErrorCategory(category ErrorCategory, provider Provider) {
	if category == "" {
		return
	}
	errorCounterMu.Lock()
	defer errorCounterMu.Unlock()
	errorCounters[errorCounterKey{category: category, provider: provider}]++
}

// LatencyP95Ms returns the in-process p95 retrieval latency in milliseconds.
func LatencyP95Ms() uint64 {
	return queryLatency.percentile(0.95)
}

// EmbedLatencyP95Ms returns the in-process p95 embed latency.
func EmbedLatencyP95Ms() uint64 {
	return embedLatency.percentile(0.95)
}

// StoreLatencyP95Ms returns the in-process p95 store latency.
func StoreLatencyP95Ms() uint64 {
	return storeLatency.percentile(0.95)
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

// RecordIndexingDLQ increments when an outbox row moves to DLQ status.
func RecordIndexingDLQ() {
	indexingDLQ.Add(1)
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
	Requests           uint64            `json:"requests"`
	Fallbacks          uint64            `json:"fallbacks"`
	ZeroHits           uint64            `json:"zeroHits"`
	FallbackRatio      float64           `json:"fallbackRatio"`
	ZeroHitRatio       float64           `json:"zeroHitRatio"`
	LatencyP50Ms       uint64            `json:"latencyP50Ms"`
	LatencyP95Ms       uint64            `json:"latencyP95Ms"`
	LatencyP99Ms       uint64            `json:"latencyP99Ms"`
	EmbedLatencyP95Ms  uint64            `json:"embedLatencyP95Ms"`
	StoreLatencyP95Ms  uint64            `json:"storeLatencyP95Ms"`
	EmbedCacheHits     uint64            `json:"embedCacheHits"`
	EmbedCacheMisses   uint64            `json:"embedCacheMisses"`
	EmbedCacheHitRatio float64           `json:"embedCacheHitRatio"`
	IndexingSuccess    uint64            `json:"indexingSuccess"`
	IndexingFailure    uint64            `json:"indexingFailure"`
	IndexingDLQ        uint64            `json:"indexingDlq"`
	BudgetMs           uint64            `json:"budgetMs"`
	SampleCount        int               `json:"sampleCount"`
	Status             string            `json:"status"`
	ErrorsByCategory   map[string]uint64 `json:"errorsByCategory"`
}

// SnapshotObservability returns in-process counters and latency percentiles.
func SnapshotObservability() ObservabilitySnapshot {
	return SnapshotObservabilityWithBreakers(nil)
}

// SnapshotObservabilityWithBreakers includes breaker pool state for status calculation.
func SnapshotObservabilityWithBreakers(pool *BreakerPool) ObservabilitySnapshot {
	req := retrievalRequests.Load()
	fb := retrievalFallback.Load()
	zh := retrievalZeroHit.Load()
	ch := embedCacheHits.Load()
	cm := embedCacheMisses.Load()
	budgetMs := uint64(QueryBudget().Milliseconds())
	embedP95 := embedLatency.percentile(0.95)
	sampleCount := queryLatency.count()

	snap := ObservabilitySnapshot{
		Requests:          req,
		Fallbacks:         fb,
		ZeroHits:          zh,
		LatencyP50Ms:      queryLatency.percentile(0.50),
		LatencyP95Ms:      queryLatency.percentile(0.95),
		LatencyP99Ms:      queryLatency.percentile(0.99),
		EmbedLatencyP95Ms: embedP95,
		StoreLatencyP95Ms: storeLatency.percentile(0.95),
		EmbedCacheHits:    ch,
		EmbedCacheMisses:  cm,
		IndexingSuccess:   indexingSuccess.Load(),
		IndexingFailure:   indexingFailure.Load(),
		IndexingDLQ:       indexingDLQ.Load(),
		BudgetMs:          budgetMs,
		SampleCount:       sampleCount,
		ErrorsByCategory:  snapshotErrorCategories(),
	}
	if req > 0 {
		snap.FallbackRatio = float64(fb) / float64(req)
		snap.ZeroHitRatio = float64(zh) / float64(req)
	}
	cacheTotal := ch + cm
	if cacheTotal > 0 {
		snap.EmbedCacheHitRatio = float64(ch) / float64(cacheTotal)
	}
	snap.Status = computeObservabilityStatus(snap, pool)
	return snap
}

func snapshotErrorCategories() map[string]uint64 {
	errorCounterMu.Lock()
	defer errorCounterMu.Unlock()
	out := make(map[string]uint64, len(errorCounters))
	for k, v := range errorCounters {
		key := string(k.category)
		if k.provider != "" {
			key += ":" + string(k.provider)
		}
		out[key] += v
	}
	return out
}

func computeObservabilityStatus(snap ObservabilitySnapshot, pool *BreakerPool) string {
	if snap.SampleCount < observabilityMinSamples {
		return "insufficient_data"
	}
	if pool != nil && pool.AnyOpenRecently(5*time.Minute) {
		return "critical"
	}
	if snap.FallbackRatio > 0.5 {
		return "critical"
	}
	if snap.FallbackRatio > 0.2 {
		return "warning"
	}
	if snap.BudgetMs > 0 && snap.EmbedLatencyP95Ms > uint64(float64(snap.BudgetMs)*0.8) {
		return "warning"
	}
	return "ok"
}

// Stats returns in-process counters (tests / debug).
func Stats() (requests, fallbacks, zeroHits uint64) {
	return retrievalRequests.Load(), retrievalFallback.Load(), retrievalZeroHit.Load()
}

// ResetObservabilityForTest clears in-process observability state (tests only).
func ResetObservabilityForTest() {
	retrievalRequests.Store(0)
	retrievalFallback.Store(0)
	retrievalZeroHit.Store(0)
	embedCacheHits.Store(0)
	embedCacheMisses.Store(0)
	indexingSuccess.Store(0)
	indexingFailure.Store(0)
	indexingDLQ.Store(0)
	embedQuotaRejected.Store(0)
	errorCounterMu.Lock()
	errorCounters = map[errorCounterKey]uint64{}
	errorCounterMu.Unlock()
	queryLatency = &latencyTracker{capacity: 2048}
	embedLatency = &latencyTracker{capacity: 2048}
	storeLatency = &latencyTracker{capacity: 2048}
}
