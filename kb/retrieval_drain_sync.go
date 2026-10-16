package kb

import (
	"context"
	"time"

	"encore.app/wabantu/shared/retrieval"
)

// ProcessPendingRetrievalOutboxSync runs indexing jobs inline (bypasses Pub/Sub). Used after rebuild when push auth fails.
func ProcessPendingRetrievalOutboxSync(ctx context.Context, tenantSchema, tenantID string, limit int) (processed, failed int, err error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	ts, err := openTenantScope(ctx, tenantSchema)
	if err != nil {
		return 0, 0, err
	}
	rows, err := ts.QueryContext(ctx, `
		SELECT id::text, entity_type, entity_id::text, version, event_type
		FROM retrieval_outbox
		WHERE status = 'pending'
		ORDER BY created_at ASC
		LIMIT $1`, limit)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var outboxID, entityType, entityID, eventType string
		var version int64
		if err := rows.Scan(&outboxID, &entityType, &entityID, &version, &eventType); err != nil {
			return processed, failed, err
		}
		job := &RetrievalIndexJob{
			TenantSchema: tenantSchema,
			TenantID:     tenantID,
			OutboxID:     outboxID,
			EntityType:   entityType,
			EntityID:     entityID,
			Version:      version,
			EventType:    eventType,
			Lane:         retrieval.IndexLaneBackfill,
			EnqueuedAt:   time.Now().UTC(),
		}
		if err := HandleRetrievalIndexJob(ctx, job); err != nil {
			failed++
			continue
		}
		processed++
	}
	return processed, failed, rows.Err()
}
