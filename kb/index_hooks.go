package kb

import (
	"context"
	"fmt"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/retrieval"
	"encore.app/wabantu/tenant"
)

// InsertKBEntryWithIndex inserts a KB row, outbox event, and publishes the index job.
// Use from kb CRUD, interview publish, CSV import, etc.
func InsertKBEntryWithIndex(
	ctx context.Context,
	tenantSchema, tenantID string,
	question, answer string,
	category *string,
	source string,
	isActive bool,
) (entryID string, err error) {
	if err := tenant.EnsureKnowledgeBaseSchema(ctx, tenantSchema); err != nil {
		return "", err
	}
	if err := tenant.EnsureRetrievalSchema(ctx, tenantSchema); err != nil {
		return "", err
	}
	ts, err := openTenantScope(ctx, tenantSchema)
	if err != nil {
		return "", err
	}
	if source == "" {
		source = "manual"
	}

	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	var version int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO knowledge_base_entry (question, answer, category, source, is_active,
		    embedding_version, embedding_status, embedding_content_hash, embedding_model, embedding_updated_at)
		VALUES ($1, $2, $3, $4, $5, 1, 'pending', $6, $7, NOW())
		RETURNING id::text, embedding_version`,
		question, answer, category, source, isActive,
		kbContentHash(question, answer), retrieval.EmbeddingModel,
	).Scan(&entryID, &version)
	if err != nil {
		return "", fmt.Errorf("insert KB: %w", err)
	}

	var outboxID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO retrieval_outbox (event_type, entity_type, entity_id, version, content_hash, status)
		VALUES ($1, $2, $3::uuid, $4, $5, 'pending')
		RETURNING id::text`,
		outboxEventIndexKB, entityTypeKB, entryID, version, kbContentHash(question, answer),
	).Scan(&outboxID)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	publishKBIndexAfterCommit(ctx, tenantSchema, tenantID, outboxID, entryID, outboxEventIndexKB, version)
	return entryID, nil
}

// EnqueueRAGBackfillForTenant queues index jobs for all active KB entries and catalog items
// that are not yet indexed (or failed).
func EnqueueRAGBackfillForTenant(ctx context.Context, tenantSchema, tenantID string, batchSize int) (kbEnqueued, catalogEnqueued int, err error) {
	if batchSize <= 0 || batchSize > 500 {
		batchSize = 200
	}
	if err := tenant.EnsureRetrievalSchema(ctx, tenantSchema); err != nil {
		return 0, 0, err
	}
	ts, err := openTenantScope(ctx, tenantSchema)
	if err != nil {
		return 0, 0, err
	}

	kbEnqueued, err = enqueueKBBackfill(ctx, ts, tenantSchema, tenantID, batchSize)
	if err != nil {
		return kbEnqueued, 0, err
	}
	catalogEnqueued, err = enqueueCatalogBackfill(ctx, ts, tenantSchema, tenantID, batchSize)
	return kbEnqueued, catalogEnqueued, err
}

func enqueueKBBackfill(ctx context.Context, ts appdb.TenantScope, tenantSchema, tenantID string, batchSize int) (int, error) {
	rows, err := ts.QueryContext(ctx, `
		SELECT id::text, question, answer, embedding_version
		FROM knowledge_base_entry
		WHERE deleted_at IS NULL AND is_active = true
		  AND embedding_status IN ('pending', 'failed')
		ORDER BY updated_at ASC
		LIMIT $1`, batchSize)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	enqueued := 0
	for rows.Next() {
		var id, question, answer string
		var version int64
		if err := rows.Scan(&id, &question, &answer, &version); err != nil {
			return enqueued, err
		}
		n, err := enqueueKBIndexOutbox(ctx, ts, tenantSchema, tenantID, id, question, answer, version)
		if err != nil {
			return enqueued, err
		}
		if n {
			enqueued++
		}
	}
	return enqueued, rows.Err()
}

func enqueueKBIndexOutbox(ctx context.Context, ts appdb.TenantScope, tenantSchema, tenantID, entryID, question, answer string, version int64) (bool, error) {
	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	hash := kbContentHash(question, answer)
	var outboxID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO retrieval_outbox (event_type, entity_type, entity_id, version, content_hash, status)
		VALUES ($1, $2, $3::uuid, $4, $5, 'pending')
		RETURNING id::text`,
		outboxEventIndexKB, entityTypeKB, entryID, version, hash,
	).Scan(&outboxID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	publishKBIndexAfterCommit(ctx, tenantSchema, tenantID, outboxID, entryID, outboxEventIndexKB, version)
	return true, nil
}

func enqueueCatalogBackfill(ctx context.Context, ts appdb.TenantScope, tenantSchema, tenantID string, batchSize int) (int, error) {
	rows, err := ts.QueryContext(ctx, `
		SELECT id::text, name, COALESCE(description,''), COALESCE(external_code,''), embedding_version
		FROM business_catalog_item
		WHERE deleted_at IS NULL AND is_active = true
		  AND embedding_status IN ('pending', 'failed')
		ORDER BY updated_at ASC
		LIMIT $1`, batchSize)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	enqueued := 0
	for rows.Next() {
		var id, name, desc, code string
		var version int64
		if err := rows.Scan(&id, &name, &desc, &code, &version); err != nil {
			return enqueued, err
		}
		if enqueueCatalogIndexOutbox(ctx, ts, tenantSchema, tenantID, id, name, desc, code, version) {
			enqueued++
		}
	}
	return enqueued, rows.Err()
}

func enqueueCatalogIndexOutbox(ctx context.Context, ts appdb.TenantScope, tenantSchema, tenantID, itemID, name, desc, code string, version int64) bool {
	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer func() { _ = tx.Rollback() }()

	hash := retrieval.ContentHash(name, desc, code)
	var outboxID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO retrieval_outbox (event_type, entity_type, entity_id, version, content_hash, status)
		VALUES ($1, $2, $3::uuid, $4, $5, 'pending')
		RETURNING id::text`,
		outboxIndexCatalog, catalogEntityType, itemID, version, hash,
	).Scan(&outboxID)
	if err != nil {
		return false
	}
	if err := tx.Commit(); err != nil {
		return false
	}
	_ = PublishRetrievalJob(ctx, &RetrievalIndexJob{
		TenantSchema: tenantSchema,
		TenantID:     tenantID,
		OutboxID:     outboxID,
		EntityType:   catalogEntityType,
		EntityID:     itemID,
		Version:      version,
		EventType:    outboxIndexCatalog,
	})
	return true
}
