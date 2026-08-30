package business

import (
	"context"
	"time"

	"encore.app/wabantu/kb"
	"encore.app/wabantu/shared/retrieval"
	"encore.app/wabantu/tenant"
)

const (
	catalogEntityType   = "catalog"
	outboxIndexCatalog  = "index_catalog"
	outboxDeleteCatalog = "delete_catalog"
)

func ensureCatalogRetrievalSchema(ctx context.Context, schema string) error {
	return tenant.EnsureRetrievalSchema(ctx, schema)
}

func catalogContentHash(name, description, code string) string {
	return retrieval.ContentHash(name, description, code)
}

func enqueueCatalogIndex(ctx context.Context, tenantSchema, tenantID, itemID, name string, description *string, code string) {
	if err := ensureCatalogRetrievalSchema(ctx, tenantSchema); err != nil {
		return
	}
	ts, err := openTenantScope(ctx, tenantSchema)
	if err != nil {
		return
	}
	desc := ""
	if description != nil {
		desc = *description
	}
	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()

	var version int64
	hash := catalogContentHash(name, desc, code)
	err = tx.QueryRowContext(ctx, `
		UPDATE business_catalog_item
		SET embedding_version = embedding_version + 1,
		    embedding_status = 'pending',
		    embedding_content_hash = $2,
		    embedding_model = $3,
		    embedding_updated_at = NOW()
		WHERE id = $1::uuid AND deleted_at IS NULL
		RETURNING embedding_version`,
		itemID, hash, retrieval.EmbeddingModel,
	).Scan(&version)
	if err != nil {
		return
	}
	var outboxID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO retrieval_outbox (event_type, entity_type, entity_id, version, content_hash, status)
		VALUES ($1, $2, $3::uuid, $4, $5, 'pending')
		RETURNING id::text`,
		outboxIndexCatalog, catalogEntityType, itemID, version, hash,
	).Scan(&outboxID)
	if err != nil {
		return
	}
	if err := tx.Commit(); err != nil {
		return
	}
	_ = kb.PublishRetrievalJob(ctx, &kb.RetrievalIndexJob{
		TenantSchema: tenantSchema,
		TenantID:     tenantID,
		OutboxID:     outboxID,
		EntityType:   catalogEntityType,
		EntityID:     itemID,
		Version:      version,
		EventType:    outboxIndexCatalog,
		EnqueuedAt:   time.Now().UTC(),
	})
}

func afterCatalogItemWritten(ctx context.Context, tenantSchema, tenantID string, item CatalogItem) {
	desc := item.Description
	enqueueCatalogIndex(ctx, tenantSchema, tenantID, item.ID, item.Name, desc, item.ExternalCode)
}
