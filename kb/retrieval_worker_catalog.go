package kb

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"encore.app/wabantu/shared/retrieval"
	appdb "encore.app/wabantu/shared/db"
	"encore.dev/rlog"
)

const catalogEntityType = "catalog"

const (
	outboxIndexCatalog  = "index_catalog"
	outboxDeleteCatalog = "delete_catalog"
)

func handleCatalogRetrievalIndexJob(ctx context.Context, job *RetrievalIndexJob) error {
	ts, err := openTenantScope(ctx, job.TenantSchema)
	if err != nil {
		return err
	}
	svc := retrieval.DefaultService()
	if svc == nil {
		return recordCatalogIndexFailure(ctx, ts, job, retrieval.ErrServiceNotConfigured)
	}
	tenantIdent := retrieval.TenantIdentity{TenantID: job.TenantID, TenantSchema: job.TenantSchema}

	var procErr error
	switch job.EventType {
	case outboxDeleteCatalog:
		procErr = retrieval.DeleteAllCatalogItemVectors(ctx, svc, tenantIdent, job.EntityID, job.Version)
	case outboxIndexCatalog:
		procErr = processCatalogIndex(ctx, ts, svc, tenantIdent, job)
	default:
		return nil
	}
	if procErr != nil {
		return recordCatalogIndexFailure(ctx, ts, job, procErr)
	}
	_ = completeOutbox(ctx, ts, job.OutboxID)
	retrieval.RecordIndexingOutcome(catalogEntityType, job.Lane, true, time.Since(job.EnqueuedAt))
	recordIndexingMetrics(catalogEntityType, job.Lane, true, indexingLagSec(job.EnqueuedAt))
	return markCatalogIndexed(ctx, ts, job.EntityID, job.Version)
}

func recordCatalogIndexFailure(ctx context.Context, ts appdb.TenantScope, job *RetrievalIndexJob, procErr error) error {
	attempts, err := nextOutboxAttempt(ctx, ts, job.OutboxID)
	if err != nil {
		return fmt.Errorf("increment outbox attempts: %w", err)
	}
	_ = failOutbox(ctx, ts, job.OutboxID, attempts, procErr.Error())
	retrieval.RecordIndexingOutcome(catalogEntityType, job.Lane, false, time.Since(job.EnqueuedAt))
	recordIndexingMetrics(catalogEntityType, job.Lane, false, indexingLagSec(job.EnqueuedAt))
	if retrieval.IsRetryableError(procErr) {
		return procErr
	}
	return nil
}

func processCatalogIndex(ctx context.Context, ts appdb.TenantScope, svc *retrieval.Service, tenant retrieval.TenantIdentity, job *RetrievalIndexJob) error {
	name, desc, code, ver, hash, ok, err := loadCatalogForIndex(ctx, ts, job.EntityID, job.Version)
	if err != nil {
		return err
	}
	if !ok {
		if err := retrieval.DeleteAllCatalogItemVectors(ctx, svc, tenant, job.EntityID, job.Version); err != nil {
			rlog.Warn("retrieval delete inactive catalog vectors failed", "item", job.EntityID, "err", err)
		}
		return nil
	}
	if err := retrieval.IndexCatalogItem(ctx, svc, retrieval.CatalogIndexInput{
		Tenant: tenant, ItemID: job.EntityID, Name: name, Description: desc,
		ExternalCode: code, Version: ver, Hash: hash,
	}); err != nil {
		return err
	}
	if err := retrieval.PurgeStaleCatalogVersions(ctx, svc, tenant, job.EntityID, ver); err != nil {
		rlog.Warn("retrieval purge stale catalog versions failed", "item", job.EntityID, "version", ver, "err", err)
	}
	return nil
}

func loadCatalogForIndex(ctx context.Context, ts appdb.TenantScope, itemID string, wantVersion int64) (name, description, code string, version int64, hash string, ok bool, err error) {
	var isActive bool
	var storedModel sql.NullString
	err = ts.QueryRowContext(ctx, `
		SELECT name, COALESCE(description,''), COALESCE(external_code,''),
		       embedding_version, COALESCE(embedding_content_hash,''), is_active, embedding_model
		FROM business_catalog_item
		WHERE id = $1::uuid AND deleted_at IS NULL`,
		itemID,
	).Scan(&name, &description, &code, &version, &hash, &isActive, &storedModel)
	if err == sql.ErrNoRows {
		return "", "", "", 0, "", false, nil
	}
	if err != nil {
		return "", "", "", 0, "", false, err
	}
	if version != wantVersion {
		return "", "", "", version, hash, false, nil
	}
	if !isActive {
		return "", "", "", version, hash, false, nil
	}
	if storedModel.Valid && !retrieval.StoredEmbeddingModelOK(storedModel.String) {
		return "", "", "", version, hash, false, fmt.Errorf("embedding model mismatch: stored %q want %q",
			storedModel.String, retrieval.EmbeddingModel)
	}
	return name, description, code, version, hash, true, nil
}

func markCatalogIndexed(ctx context.Context, ts interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, itemID string, version int64) error {
	_, err := ts.ExecContext(ctx, `
		UPDATE business_catalog_item
		SET embedding_status = 'indexed', embedding_indexed_at = NOW()
		WHERE id = $1::uuid AND embedding_version = $2`, itemID, version)
	if err != nil {
		return fmt.Errorf("mark catalog indexed: %w", err)
	}
	return nil
}
