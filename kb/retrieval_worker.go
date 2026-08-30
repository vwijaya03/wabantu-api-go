package kb

import (
	"context"
	"fmt"
	"time"

	"encore.dev/pubsub"
	"encore.dev/rlog"

	"encore.app/wabantu/shared/retrieval"
	appdb "encore.app/wabantu/shared/db"
)

// RetrievalIndexJob is processed by the indexing worker (at-least-once).
type RetrievalIndexJob struct {
	TenantSchema string    `json:"tenantSchema"`
	TenantID     string    `json:"tenantId"`
	OutboxID     string    `json:"outboxId"`
	EntityType   string    `json:"entityType"`
	EntityID     string    `json:"entityId"`
	Version      int64     `json:"version"`
	EventType    string    `json:"eventType"`
	Lane         string    `json:"lane,omitempty"` // live | backfill
	EnqueuedAt   time.Time `json:"enqueuedAt"`
}

var RetrievalIndexTopic = pubsub.NewTopic[*RetrievalIndexJob]("retrieval-index", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

var _ = pubsub.NewSubscription(RetrievalIndexTopic, "retrieval-index-worker", pubsub.SubscriptionConfig[*RetrievalIndexJob]{
	Handler:     handleRetrievalIndexJob,
	RetryPolicy: &pubsub.RetryPolicy{MaxRetries: 5},
})

func handleRetrievalIndexJob(ctx context.Context, job *RetrievalIndexJob) error {
	if job == nil {
		return nil
	}
	lane := job.Lane
	if lane == "" {
		lane = retrieval.IndexLaneLive
	}
	var err error
	switch job.EntityType {
	case entityTypeKB:
		err = retrieval.WithIndexingLane(ctx, lane, func() error {
			return handleKBRetrievalIndexJob(ctx, job)
		})
	case catalogEntityType:
		err = retrieval.WithIndexingLane(ctx, lane, func() error {
			return handleCatalogRetrievalIndexJob(ctx, job)
		})
	default:
		return nil
	}
	return err
}

func indexingLagSec(enqueuedAt time.Time) uint64 {
	lag := time.Since(enqueuedAt)
	if lag <= 0 {
		return 0
	}
	sec := uint64(lag.Seconds())
	if sec == 0 {
		return 1
	}
	return sec
}

func handleKBRetrievalIndexJob(ctx context.Context, job *RetrievalIndexJob) error {
	ts, err := openTenantScope(ctx, job.TenantSchema)
	if err != nil {
		return err
	}
	svc := retrieval.DefaultService()
	if svc == nil {
		svc = retrieval.NewService(retrieval.NewMockEmbedder(), retrieval.NewMemoryStore())
	}
	tenantIdent := retrieval.TenantIdentity{TenantID: job.TenantID, TenantSchema: job.TenantSchema}

	var procErr error
	switch job.EventType {
	case outboxEventDeleteKB:
		procErr = retrieval.DeleteKBEntryVectors(ctx, svc, tenantIdent, job.EntityID, job.Version)
	case outboxEventIndexKB:
		procErr = processKBIndex(ctx, ts, svc, tenantIdent, job)
	default:
		return nil
	}

	if procErr != nil {
		attempts := 1
		_ = failOutbox(ctx, ts, job.OutboxID, attempts, procErr.Error())
		_ = markKBIndexFailed(ctx, ts, job.EntityID, job.Version, attempts, procErr.Error())
		retrieval.RecordIndexingOutcome(entityTypeKB, job.Lane, false, time.Since(job.EnqueuedAt))
		recordIndexingMetrics(entityTypeKB, job.Lane, false, indexingLagSec(job.EnqueuedAt))
		if retrieval.IsRetryableError(procErr) {
			return procErr
		}
		rlog.Warn("retrieval index permanent failure", "entity", job.EntityID, "err", procErr)
		return nil
	}
	_ = completeOutbox(ctx, ts, job.OutboxID)
	retrieval.RecordIndexingOutcome(entityTypeKB, job.Lane, true, time.Since(job.EnqueuedAt))
	recordIndexingMetrics(entityTypeKB, job.Lane, true, indexingLagSec(job.EnqueuedAt))
	return markKBIndexed(ctx, ts, job.EntityID, job.Version)
}

func processKBIndex(ctx context.Context, ts appdb.TenantScope, svc *retrieval.Service, tenant retrieval.TenantIdentity, job *RetrievalIndexJob) error {
	q, a, cat, ver, hash, ok, err := loadKBForIndex(ctx, ts, job.EntityID, job.Version)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if err := retrieval.IndexKBEntry(ctx, svc, retrieval.KBIndexInput{
		Tenant: tenant, EntryID: job.EntityID, Question: q, Answer: a,
		Category: cat, Version: ver, Hash: hash,
	}); err != nil {
		return err
	}
	if ver > 1 {
		_ = svc.DeleteKB(ctx, tenant, job.EntityID, ver-1)
	}
	return nil
}

// ReindexRequest triggers checkpointed backfill for KB vectors.
type ReindexRequest struct {
	BatchSize int `json:"batchSize"`
}

type ReindexResponse struct {
	Enqueued int `json:"enqueued"`
}

//encore:api auth method=POST path=/api/v1/knowledge-base/reindex
func Reindex(ctx context.Context, req *ReindexRequest) (*ReindexResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if err := ensureRetrievalSchema(ctx, u.TenantSchema); err != nil {
		return nil, err
	}
	ts, err := openTenantScope(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	batch := 100
	if req != nil && req.BatchSize > 0 && req.BatchSize <= 500 {
		batch = req.BatchSize
	}
	rows, err := ts.QueryContext(ctx, `
		SELECT id::text, question, answer, embedding_version
		FROM knowledge_base_entry
		WHERE deleted_at IS NULL AND is_active = true
		  AND embedding_status IN ('pending', 'failed')
		ORDER BY updated_at ASC
		LIMIT $1`, batch)
	if err != nil {
		return nil, fmt.Errorf("reindex query: %w", err)
	}
	defer rows.Close()

	enqueued := 0
	for rows.Next() {
		var id, question, answer string
		var version int64
		if err := rows.Scan(&id, &question, &answer, &version); err != nil {
			return nil, err
		}
		tx, err := ts.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		hash := kbContentHash(question, answer)
		var outboxID string
		err = tx.QueryRowContext(ctx, `
			INSERT INTO retrieval_outbox (event_type, entity_type, entity_id, version, content_hash, status)
			VALUES ($1, $2, $3::uuid, $4, $5, 'pending')
			RETURNING id::text`, outboxEventIndexKB, entityTypeKB, id, version, hash,
		).Scan(&outboxID)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		publishKBIndexAfterCommit(ctx, u.TenantSchema, u.TenantID, outboxID, id, outboxEventIndexKB, version, retrieval.IndexLaneBackfill)
		enqueued++
	}
	return &ReindexResponse{Enqueued: enqueued}, rows.Err()
}
