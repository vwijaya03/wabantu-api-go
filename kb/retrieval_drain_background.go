package kb

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"encore.dev/rlog"
)

// RETRIEVAL_BACKGROUND_DRAIN=1 enables a bounded in-process drain of the retrieval outbox after a
// rebuild response has been sent. Intended for Cloud Run (lomba) where Pub/Sub push can fail JWT
// validation; Encore Cloud keeps using the native retrieval-index-worker subscription.
const backgroundDrainEnv = "RETRIEVAL_BACKGROUND_DRAIN"

const (
	backgroundDrainMaxConcurrent = 2                // tenants drained in parallel per process
	backgroundDrainBatch         = 100              // outbox rows per round
	backgroundDrainMaxRounds     = 50               // hard stop: 5000 rows per trigger
	backgroundDrainTimeout       = 10 * time.Minute // wall-clock cap per tenant drain
)

var (
	backgroundDrainSem      = make(chan struct{}, backgroundDrainMaxConcurrent)
	backgroundDrainMu       sync.Mutex
	backgroundDrainInFlight = map[string]struct{}{} // one drain per tenant
)

// drainRunner is swapped in tests.
var drainRunner = drainRetrievalOutbox

func backgroundDrainEnabled() bool {
	v := strings.TrimSpace(os.Getenv(backgroundDrainEnv))
	return v == "1" || strings.EqualFold(v, "true")
}

// scheduleBackgroundRetrievalDrain starts a detached drain for one tenant and returns immediately.
// It reports false when disabled, already running for that tenant, or the process is at its
// concurrency cap (the outbox stays pending for the Pub/Sub worker or a later trigger).
func scheduleBackgroundRetrievalDrain(tenantSchema, tenantID string) bool {
	if !backgroundDrainEnabled() || tenantSchema == "" || tenantID == "" {
		return false
	}
	backgroundDrainMu.Lock()
	if _, busy := backgroundDrainInFlight[tenantID]; busy {
		backgroundDrainMu.Unlock()
		return false
	}
	select {
	case backgroundDrainSem <- struct{}{}:
	default:
		backgroundDrainMu.Unlock()
		return false
	}
	backgroundDrainInFlight[tenantID] = struct{}{}
	backgroundDrainMu.Unlock()

	go func() {
		defer func() {
			backgroundDrainMu.Lock()
			delete(backgroundDrainInFlight, tenantID)
			backgroundDrainMu.Unlock()
			<-backgroundDrainSem
		}()
		ctx, cancel := context.WithTimeout(context.Background(), backgroundDrainTimeout)
		defer cancel()
		drainRunner(ctx, tenantSchema, tenantID)
	}()
	return true
}

// drainRetrievalOutbox processes pending outbox rows in batches until none are left or a cap is hit.
func drainRetrievalOutbox(ctx context.Context, tenantSchema, tenantID string) {
	totalProcessed, totalFailed := 0, 0
	for round := 0; round < backgroundDrainMaxRounds; round++ {
		if ctx.Err() != nil {
			break
		}
		processed, failed, err := ProcessPendingRetrievalOutboxSync(ctx, tenantSchema, tenantID, backgroundDrainBatch)
		totalProcessed += processed
		totalFailed += failed
		if err != nil {
			rlog.Warn("retrieval background drain failed", "tenantId", tenantID, "err", err)
			break
		}
		// Failed rows stay pending for retry by the worker; stop when a round made no progress.
		if processed == 0 {
			break
		}
	}
	rlog.Info("retrieval background drain done", "tenantId", tenantID, "processed", totalProcessed, "failed", totalFailed)
}
