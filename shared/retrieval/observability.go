package retrieval

import (
	"context"
	"sync/atomic"

	"encore.dev/rlog"
)

var (
	retrievalRequests atomic.Uint64
	retrievalFallback atomic.Uint64
	retrievalZeroHit  atomic.Uint64
)

// LogQuery records retrieval observability fields (structured logs for rollout monitoring).
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

// Stats returns in-process counters (tests / debug).
func Stats() (requests, fallbacks, zeroHits uint64) {
	return retrievalRequests.Load(), retrievalFallback.Load(), retrievalZeroHit.Load()
}
