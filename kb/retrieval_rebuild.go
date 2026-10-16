package kb

import (
	"context"
	"fmt"

	"encore.dev/rlog"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/retrieval"
	"encore.app/wabantu/tenant"
)

// RebuildTenantRetrievalOptions controls a full retrieval re-index for one tenant.
type RebuildTenantRetrievalOptions struct {
	WipePinecone   bool
	IncludeKB      bool
	IncludeCatalog bool
}

// RebuildTenantRetrievalResult summarizes reset and enqueue counts.
type RebuildTenantRetrievalResult struct {
	KBReset         int64 `json:"kbReset"`
	CatalogReset    int64 `json:"catalogReset"`
	KBEnqueued      int   `json:"kbEnqueued"`
	CatalogEnqueued int   `json:"catalogEnqueued"`
	PineconeWiped   bool  `json:"pineconeWiped"`
}

// RebuildTenantRetrievalIndex wipes optional Pinecone data, resets embedding rows,
// supersedes stale outbox work, and enqueues a RAG backfill for the tenant.
func RebuildTenantRetrievalIndex(ctx context.Context, tenantSchema, tenantID string, opts RebuildTenantRetrievalOptions) (*RebuildTenantRetrievalResult, error) {
	if tenantSchema == "" || tenantID == "" {
		return nil, fmt.Errorf("tenantSchema and tenantID required")
	}
	if !opts.IncludeKB && !opts.IncludeCatalog {
		return nil, fmt.Errorf("at least one of IncludeKB or IncludeCatalog must be true")
	}

	if err := tenant.EnsureRetrievalSchema(ctx, tenantSchema); err != nil {
		return nil, err
	}

	result := &RebuildTenantRetrievalResult{}

	if opts.WipePinecone {
		if svc := retrieval.DefaultService(); svc != nil {
			tenantIdent := retrieval.TenantIdentity{TenantID: tenantID, TenantSchema: tenantSchema}
			if err := retrieval.DeleteTenantNamespace(ctx, svc, tenantIdent); err != nil {
				rlog.Warn("rebuild: delete pinecone namespace failed", "tenantId", tenantID, "schema", tenantSchema, "err", err)
			} else {
				result.PineconeWiped = true
			}
		}
	}

	ts, err := openTenantScope(ctx, tenantSchema)
	if err != nil {
		return nil, err
	}

	if opts.IncludeKB {
		res, err := ts.ExecContext(ctx, `
			UPDATE knowledge_base_entry
			SET embedding_status = 'pending',
			    embedding_attempts = 0,
			    embedding_last_error = NULL
			WHERE deleted_at IS NULL AND is_active = true
			  AND embedding_status IN ('indexed', 'failed', 'dlq')`)
		if err != nil {
			return nil, err
		}
		result.KBReset, _ = res.RowsAffected()
	}

	if opts.IncludeCatalog {
		res, err := ts.ExecContext(ctx, `
			UPDATE business_catalog_item
			SET embedding_status = 'pending',
			    embedding_attempts = 0,
			    embedding_last_error = NULL
			WHERE deleted_at IS NULL AND is_active = true
			  AND embedding_status IN ('indexed', 'failed', 'dlq')`)
		if err != nil {
			return nil, err
		}
		result.CatalogReset, _ = res.RowsAffected()
	}

	if err := supersedeStaleOutboxForRebuild(ctx, ts, opts.IncludeKB, opts.IncludeCatalog); err != nil {
		return nil, err
	}

	kbN, catN, err := EnqueueRAGBackfillForTenant(ctx, tenantSchema, tenantID, 500)
	if err != nil {
		return nil, err
	}
	result.KBEnqueued = kbN
	result.CatalogEnqueued = catN
	return result, nil
}

func supersedeStaleOutboxForRebuild(ctx context.Context, ts appdb.TenantScope, includeKB, includeCatalog bool) error {
	entityTypes := entityTypesForRebuild(includeKB, includeCatalog)
	if len(entityTypes) == 0 {
		return nil
	}

	var query string
	var args []any
	switch len(entityTypes) {
	case 1:
		query = `
			UPDATE retrieval_outbox
			SET status = 'done', processed_at = NOW(), updated_at = NOW(), last_error = NULL
			WHERE status IN ('pending', 'failed') AND entity_type = $1`
		args = []any{entityTypes[0]}
	case 2:
		query = `
			UPDATE retrieval_outbox
			SET status = 'done', processed_at = NOW(), updated_at = NOW(), last_error = NULL
			WHERE status IN ('pending', 'failed') AND entity_type IN ($1, $2)`
		args = []any{entityTypes[0], entityTypes[1]}
	default:
		return fmt.Errorf("unexpected entity type count %d", len(entityTypes))
	}
	_, err := ts.ExecContext(ctx, query, args...)
	return err
}

func entityTypesForRebuild(includeKB, includeCatalog bool) []string {
	types := make([]string, 0, 2)
	if includeKB {
		types = append(types, entityTypeKB)
	}
	if includeCatalog {
		types = append(types, catalogEntityType)
	}
	return types
}
