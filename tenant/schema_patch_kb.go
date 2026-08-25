package tenant

import (
	"context"
	"database/sql"

	"encore.dev"

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
	conn, err := TenantConn(ctx, schemaName)
	if err != nil {
		return err
	}
	defer conn.Close()
	return alwaysApplyKnowledgeBasePatch(ctx, conn)
}

func alwaysApplyKnowledgeBasePatch(ctx context.Context, conn *sql.Conn) error {
	schemaName, err := tenantSchemaFromConn(ctx, conn)
	if err != nil {
		return err
	}
	exists, err := tenantschema.TableExists(ctx, conn, schemaName, "knowledge_base_entry")
	if err != nil || exists {
		return err
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return ensureCloudAdminDDLForConn(ctx, conn)
	}
	_, err = conn.ExecContext(ctx, knowledgeBaseEntryPatchSQL)
	return err
}
