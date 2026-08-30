package kb

import (
	"context"
	"database/sql"
	"fmt"

	"encore.app/wabantu/shared/retrieval"
	appdb "encore.app/wabantu/shared/db"
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
		svc = retrieval.NewService(retrieval.NewMockEmbedder(), retrieval.NewMemoryStore())
	}
	tenantIdent := retrieval.TenantIdentity{TenantID: job.TenantID, TenantSchema: job.TenantSchema}

	var procErr error
	switch job.EventType {
	case outboxDeleteCatalog:
		ns, err := retrieval.Namespace(tenantIdent)
		if err != nil {
			return err
		}
		id := retrieval.CatalogVectorID(job.EntityID, job.Version, 0)
		procErr = svc.Store.DeleteIDs(ctx, ns, []string{id})
	case outboxIndexCatalog:
		procErr = processCatalogIndex(ctx, ts, svc, tenantIdent, job)
	default:
		return nil
	}
	if procErr != nil {
		attempts := 1
		_ = failOutbox(ctx, ts, job.OutboxID, attempts, procErr.Error())
		if retrieval.IsRetryableError(procErr) {
			return procErr
		}
		return nil
	}
	_ = completeOutbox(ctx, ts, job.OutboxID)
	return markCatalogIndexed(ctx, ts, job.EntityID, job.Version)
}

func processCatalogIndex(ctx context.Context, ts appdb.TenantScope, svc *retrieval.Service, tenant retrieval.TenantIdentity, job *RetrievalIndexJob) error {
	name, desc, code, ver, hash, ok, err := loadCatalogForIndex(ctx, ts, job.EntityID, job.Version)
	if err != nil || !ok {
		return err
	}
	if err := retrieval.IndexCatalogItem(ctx, svc, retrieval.CatalogIndexInput{
		Tenant: tenant, ItemID: job.EntityID, Name: name, Description: desc,
		ExternalCode: code, Version: ver, Hash: hash,
	}); err != nil {
		return err
	}
	if ver > 1 {
		id := retrieval.CatalogVectorID(job.EntityID, ver-1, 0)
		ns, _ := retrieval.Namespace(tenant)
		_ = svc.Store.DeleteIDs(ctx, ns, []string{id})
	}
	return nil
}

func loadCatalogForIndex(ctx context.Context, ts appdb.TenantScope, itemID string, wantVersion int64) (name, description, code string, version int64, hash string, ok bool, err error) {
	err = ts.QueryRowContext(ctx, `
		SELECT name, COALESCE(description,''), COALESCE(external_code,''),
		       embedding_version, COALESCE(embedding_content_hash,'')
		FROM business_catalog_item
		WHERE id = $1::uuid AND deleted_at IS NULL`,
		itemID,
	).Scan(&name, &description, &code, &version, &hash)
	if err == sql.ErrNoRows {
		return "", "", "", 0, "", false, nil
	}
	if err != nil {
		return "", "", "", 0, "", false, err
	}
	if version != wantVersion {
		return "", "", "", version, hash, false, nil
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
