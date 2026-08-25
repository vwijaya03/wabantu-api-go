package tenant

import (
	"context"
	"fmt"

	"encore.dev"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/tenantschema"
)

const knowledgeBaseEntryPatchSQL = `
CREATE TABLE IF NOT EXISTS knowledge_base_entry (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question    VARCHAR(500) NOT NULL,
    answer      TEXT         NOT NULL,
    category    VARCHAR(60),
    is_active   BOOLEAN      NOT NULL DEFAULT true,
    source      VARCHAR(20)  NOT NULL DEFAULT 'manual',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,
    deleted_by  UUID
);
CREATE INDEX IF NOT EXISTS idx_kb_entry_category
    ON knowledge_base_entry(category);
`

// EnsureKnowledgeBaseSchema creates knowledge_base_entry on older tenant schemas (idempotent).
func EnsureKnowledgeBaseSchema(ctx context.Context, schemaName string) error {
	pool := DataDB.Stdlib()
	return alwaysApplyKnowledgeBasePatch(ctx, pool, schemaName)
}

func alwaysApplyKnowledgeBasePatch(ctx context.Context, q appdb.TenantQuerier, schemaName string) error {
	exists, err := tenantschema.TableExists(ctx, q, schemaName, "knowledge_base_entry")
	if err != nil || exists {
		return err
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return EnsureCloudAdminTenantDDL(ctx, schemaName)
	}
	sch := appdb.SchemaSQL{Schema: schemaName}
	kbTable := sch.T("knowledge_base_entry")
	_, err = q.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
		    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		    question    VARCHAR(500) NOT NULL,
		    answer      TEXT         NOT NULL,
		    category    VARCHAR(60),
		    is_active   BOOLEAN      NOT NULL DEFAULT true,
		    source      VARCHAR(20)  NOT NULL DEFAULT 'manual',
		    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
		    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
		    deleted_at  TIMESTAMPTZ,
		    deleted_by  UUID
		);
		CREATE INDEX IF NOT EXISTS idx_kb_entry_category
		    ON %s(category);
	`, kbTable, kbTable))
	return err
}
