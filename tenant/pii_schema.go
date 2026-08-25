package tenant

import (
	"context"
	"database/sql"
	"fmt"

	"encore.app/wabantu/shared/tenantschema"
	"encore.dev"
)

// piiConstraintPatchSQL drops legacy uniqueness that conflicts with encrypted placeholders.
const piiConstraintPatchSQL = `
ALTER TABLE contact DROP CONSTRAINT IF EXISTS contact_phone_number_key;
ALTER TABLE contact ALTER COLUMN phone_number DROP NOT NULL;
`

// RunPIISchemaPatchesOnConn applies PII DDL on an open tenant connection.
func RunPIISchemaPatchesOnConn(ctx context.Context, conn *sql.Conn) error {
	schema, err := SchemaFromConn(ctx, conn)
	if err != nil {
		return err
	}
	ready, err := tenantschema.PIIReady(ctx, conn, schema)
	if err != nil {
		return err
	}
	if !ready {
		if encore.Meta().Environment.Cloud != encore.CloudLocal {
			if err := ensureCloudAdminDDLForConn(ctx, conn); err != nil {
				return err
			}
		} else if _, err = conn.ExecContext(ctx, tenantschema.PIISchemaPatchSQL); err != nil {
			return err
		}
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return nil
	}
	_, err = conn.ExecContext(ctx, piiConstraintPatchSQL)
	return err
}

// RunPIISchemaPatches applies PII encryption column DDL (idempotent).
func RunPIISchemaPatches(ctx context.Context, schemaName string) error {
	if !schemaNameRe.MatchString(schemaName) {
		return fmt.Errorf("invalid schema name: %q", schemaName)
	}
	conn, err := TenantConn(ctx, schemaName)
	if err != nil {
		return err
	}
	defer conn.Close()
	return RunPIISchemaPatchesOnConn(ctx, conn)
}
