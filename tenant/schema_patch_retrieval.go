package tenant

import (
	"context"
	"fmt"

	"encore.dev"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/tenantschema"
)

const retrievalOutboxPatchSQL = `
CREATE TABLE IF NOT EXISTS retrieval_outbox (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type      VARCHAR(32) NOT NULL,
    entity_type     VARCHAR(32) NOT NULL,
    entity_id       UUID NOT NULL,
    version         BIGINT NOT NULL DEFAULT 1,
    content_hash    VARCHAR(64),
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    attempts        INT NOT NULL DEFAULT 0,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_retrieval_outbox_pending
    ON retrieval_outbox(status, created_at)
    WHERE status IN ('pending', 'failed');

ALTER TABLE knowledge_base_entry ADD COLUMN IF NOT EXISTS embedding_status VARCHAR(20) NOT NULL DEFAULT 'pending';
ALTER TABLE knowledge_base_entry ADD COLUMN IF NOT EXISTS embedding_version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE knowledge_base_entry ADD COLUMN IF NOT EXISTS embedding_content_hash VARCHAR(64);
ALTER TABLE knowledge_base_entry ADD COLUMN IF NOT EXISTS embedding_model VARCHAR(64);
ALTER TABLE knowledge_base_entry ADD COLUMN IF NOT EXISTS embedding_attempts INT NOT NULL DEFAULT 0;
ALTER TABLE knowledge_base_entry ADD COLUMN IF NOT EXISTS embedding_last_error TEXT;
ALTER TABLE knowledge_base_entry ADD COLUMN IF NOT EXISTS embedding_updated_at TIMESTAMPTZ;
ALTER TABLE knowledge_base_entry ADD COLUMN IF NOT EXISTS embedding_indexed_at TIMESTAMPTZ;

ALTER TABLE business_catalog_item ADD COLUMN IF NOT EXISTS embedding_status VARCHAR(20) NOT NULL DEFAULT 'pending';
ALTER TABLE business_catalog_item ADD COLUMN IF NOT EXISTS embedding_version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE business_catalog_item ADD COLUMN IF NOT EXISTS embedding_content_hash VARCHAR(64);
ALTER TABLE business_catalog_item ADD COLUMN IF NOT EXISTS embedding_model VARCHAR(64);
ALTER TABLE business_catalog_item ADD COLUMN IF NOT EXISTS embedding_attempts INT NOT NULL DEFAULT 0;
ALTER TABLE business_catalog_item ADD COLUMN IF NOT EXISTS embedding_last_error TEXT;
ALTER TABLE business_catalog_item ADD COLUMN IF NOT EXISTS embedding_updated_at TIMESTAMPTZ;
ALTER TABLE business_catalog_item ADD COLUMN IF NOT EXISTS embedding_indexed_at TIMESTAMPTZ;
`

// EnsureRetrievalSchema applies retrieval outbox + embedding columns (idempotent).
func EnsureRetrievalSchema(ctx context.Context, schemaName string) error {
	pool := DataDB.Stdlib()
	return alwaysApplyRetrievalPatch(ctx, pool, schemaName)
}

func alwaysApplyRetrievalPatch(ctx context.Context, q any, schemaName string) error {
	exists, err := tenantschema.TableExists(ctx, q, schemaName, "retrieval_outbox")
	if err != nil {
		return err
	}
	if exists {
		return applyKBEmbeddingColumns(ctx, q, schemaName)
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return EnsureCloudAdminTenantDDL(ctx, schemaName)
	}
	sch := appdb.SchemaSQL{Schema: schemaName}
	outbox := sch.T("retrieval_outbox")
	kb := sch.T("knowledge_base_entry")
	cat := sch.T("business_catalog_item")
	querier := tenantschema.Q(q)
	_, err = querier.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
		    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		    event_type      VARCHAR(32) NOT NULL,
		    entity_type     VARCHAR(32) NOT NULL,
		    entity_id       UUID NOT NULL,
		    version         BIGINT NOT NULL DEFAULT 1,
		    content_hash    VARCHAR(64),
		    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
		    attempts        INT NOT NULL DEFAULT 0,
		    last_error      TEXT,
		    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
		    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
		    processed_at    TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS idx_retrieval_outbox_pending
		    ON %s(status, created_at)
		    WHERE status IN ('pending', 'failed');
	`, outbox, outbox))
	if err != nil {
		return err
	}
	return applyKBEmbeddingColumnsOnTables(ctx, querier, kb, cat)
}

func applyKBEmbeddingColumns(ctx context.Context, q any, schemaName string) error {
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return nil
	}
	sch := appdb.SchemaSQL{Schema: schemaName}
	return applyKBEmbeddingColumnsOnTables(ctx, tenantschema.Q(q), sch.T("knowledge_base_entry"), sch.T("business_catalog_item"))
}

func applyKBEmbeddingColumnsOnTables(ctx context.Context, q tenantschema.Querier, kbTable, catTable string) error {
	stmts := []string{
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_status VARCHAR(20) NOT NULL DEFAULT 'pending'`, kbTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_version BIGINT NOT NULL DEFAULT 0`, kbTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_content_hash VARCHAR(64)`, kbTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_model VARCHAR(64)`, kbTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_attempts INT NOT NULL DEFAULT 0`, kbTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_last_error TEXT`, kbTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_updated_at TIMESTAMPTZ`, kbTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_indexed_at TIMESTAMPTZ`, kbTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_status VARCHAR(20) NOT NULL DEFAULT 'pending'`, catTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_version BIGINT NOT NULL DEFAULT 0`, catTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_content_hash VARCHAR(64)`, catTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_model VARCHAR(64)`, catTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_attempts INT NOT NULL DEFAULT 0`, catTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_last_error TEXT`, catTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_updated_at TIMESTAMPTZ`, catTable),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS embedding_indexed_at TIMESTAMPTZ`, catTable),
	}
	for _, s := range stmts {
		if _, err := q.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}
