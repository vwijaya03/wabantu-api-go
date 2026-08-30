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

func afterCatalogItemDeleted(ctx context.Context, tenantSchema, tenantID, itemID string, version int64) {
	if err := ensureCatalogRetrievalSchema(ctx, tenantSchema); err != nil {
		return
	}
	ts, err := openTenantScope(ctx, tenantSchema)
	if err != nil {
		return
	}
	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()

	var outboxID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO retrieval_outbox (event_type, entity_type, entity_id, version, content_hash, status)
		VALUES ($1, $2, $3::uuid, $4, '', 'pending')
		RETURNING id::text`,
		outboxDeleteCatalog, catalogEntityType, itemID, version,
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
		EventType:    outboxDeleteCatalog,
		EnqueuedAt:   time.Now().UTC(),
	})
}

// IndexImportedCatalog enqueues vector indexing for a row imported via CSV/XLSX.
func IndexImportedCatalog(ctx context.Context, tenantSchema, tenantID, source, externalCode string) error {
	ts, err := openTenantScope(ctx, tenantSchema)
	if err != nil {
		return err
	}
	row := ts.QueryRowContext(ctx, `
		SELECT id, external_code, name, description, sell_price, sell_unit,
		       is_active, barcode, source, created_at, updated_at
		FROM business_catalog_item
		WHERE source = $1 AND external_code = $2 AND deleted_at IS NULL`,
		source, externalCode)
	item, err := scanCatalog(row.Scan)
	if err != nil {
		return err
	}
	afterCatalogItemWritten(ctx, tenantSchema, tenantID, item)
	return nil
}

func afterCatalogItemWritten(ctx context.Context, tenantSchema, tenantID string, item CatalogItem) {
	desc := item.Description
	enqueueCatalogIndex(ctx, tenantSchema, tenantID, item.ID, item.Name, desc, item.ExternalCode)
}
