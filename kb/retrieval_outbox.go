package kb

import (
	"context"
	"database/sql"
	"time"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/retrieval"
	"encore.app/wabantu/tenant"
)

const (
	outboxEventIndexKB  = "index_kb"
	outboxEventDeleteKB = "delete_kb"
	entityTypeKB        = "kb"
)

func ensureRetrievalSchema(ctx context.Context, schema string) error {
	return tenant.EnsureRetrievalSchema(ctx, schema)
}

func kbContentHash(question, answer string) string {
	return retrieval.ContentHash(question, answer)
}

func enqueueKBOutboxTx(ctx context.Context, tx *sql.Tx, eventType, entryID string, version int64, hash string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO retrieval_outbox (event_type, entity_type, entity_id, version, content_hash, status)
		VALUES ($1, $2, $3::uuid, $4, $5, 'pending')`,
		eventType, entityTypeKB, entryID, version, hash)
	return err
}

func bumpKBEmbeddingPendingTx(ctx context.Context, tx *sql.Tx, entryID, question, answer string) (int64, error) {
	hash := kbContentHash(question, answer)
	var version int64
	err := tx.QueryRowContext(ctx, `
		UPDATE knowledge_base_entry
		SET embedding_version = embedding_version + 1,
		    embedding_status = 'pending',
		    embedding_content_hash = $2,
		    embedding_model = $3,
		    embedding_updated_at = NOW(),
		    embedding_attempts = 0,
		    embedding_last_error = NULL
		WHERE id = $1::uuid AND deleted_at IS NULL
		RETURNING embedding_version`,
		entryID, hash, retrieval.EmbeddingModel,
	).Scan(&version)
	return version, err
}

func markKBIndexed(ctx context.Context, ts interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, entryID string, version int64) error {
	_, err := ts.ExecContext(ctx, `
		UPDATE knowledge_base_entry
		SET embedding_status = 'indexed',
		    embedding_indexed_at = NOW(),
		    embedding_last_error = NULL
		WHERE id = $1::uuid AND embedding_version = $2 AND deleted_at IS NULL`,
		entryID, version)
	return err
}

func markKBIndexFailed(ctx context.Context, ts interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, entryID string, version int64, attempts int, errMsg string) error {
	status := "failed"
	if retrieval.ShouldDLQ(attempts) {
		status = "dlq"
	}
	_, err := ts.ExecContext(ctx, `
		UPDATE knowledge_base_entry
		SET embedding_status = $4,
		    embedding_attempts = $3,
		    embedding_last_error = $5
		WHERE id = $1::uuid AND embedding_version = $2 AND deleted_at IS NULL`,
		entryID, version, attempts, status, errMsg)
	return err
}

func loadKBForIndex(ctx context.Context, ts appdb.TenantScope, entryID string, wantVersion int64) (question, answer, category string, version int64, hash string, ok bool, err error) {
	err = ts.QueryRowContext(ctx, `
		SELECT question, answer, COALESCE(category,''), embedding_version, COALESCE(embedding_content_hash,'')
		FROM knowledge_base_entry
		WHERE id = $1::uuid AND deleted_at IS NULL`,
		entryID,
	).Scan(&question, &answer, &category, &version, &hash)
	if err == sql.ErrNoRows {
		return "", "", "", 0, "", false, nil
	}
	if err != nil {
		return "", "", "", 0, "", false, err
	}
	if version != wantVersion {
		return "", "", "", version, hash, false, nil
	}
	return question, answer, category, version, hash, true, nil
}

func completeOutbox(ctx context.Context, ts interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, outboxID string) error {
	_, err := ts.ExecContext(ctx, `
		UPDATE retrieval_outbox
		SET status = 'done', processed_at = NOW(), updated_at = NOW()
		WHERE id = $1::uuid`, outboxID)
	return err
}

func failOutbox(ctx context.Context, ts interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, outboxID string, attempts int, errMsg string) error {
	status := "failed"
	if retrieval.ShouldDLQ(attempts) {
		status = "dlq"
	}
	_, err := ts.ExecContext(ctx, `
		UPDATE retrieval_outbox
		SET status = $2, attempts = $3, last_error = $4, updated_at = NOW()
		WHERE id = $1::uuid`, outboxID, status, attempts, errMsg)
	return err
}

func publishKBIndexAfterCommit(ctx context.Context, tenantSchema, tenantID, outboxID, entryID, eventType string, version int64, lane string) {
	if lane == "" {
		lane = retrieval.IndexLaneLive
	}
	_, _ = RetrievalIndexTopic.Publish(ctx, &RetrievalIndexJob{
		TenantSchema: tenantSchema,
		TenantID:     tenantID,
		OutboxID:     outboxID,
		EntityType:   entityTypeKB,
		EntityID:     entryID,
		Version:      version,
		EventType:    eventType,
		Lane:         lane,
		EnqueuedAt:   time.Now().UTC(),
	})
}
