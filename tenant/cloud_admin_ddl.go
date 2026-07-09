package tenant

import (
	"context"
	"fmt"

	"encore.dev"
)

// DropTenantSchema permanently removes a tenant schema (destructive).
// On Encore Cloud the runtime role lacks schema ownership; we SET LOCAL ROLE
// db_tenant_admin inside a transaction (requires GRANT db_tenant_admin TO runtime role).
func DropTenantSchema(ctx context.Context, schemaName string) error {
	if !schemaNameRe.MatchString(schemaName) {
		return fmt.Errorf("invalid schema name: %q", schemaName)
	}

	tx, err := DataDB.Stdlib().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin drop schema tx: %w", err)
	}
	defer tx.Rollback()

	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL ROLE %s", cloudDBTenantAdmin)); err != nil {
			return fmt.Errorf(
				"set tenant admin role (jalankan ./scripts/fix-cloud-db-grants.sh %s jika belum): %w",
				encore.Meta().Environment.Name,
				err,
			)
		}
	}

	qSchema := quoteIdent(schemaName)
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, qSchema)); err != nil {
		return fmt.Errorf("drop schema %s: %w", schemaName, err)
	}
	return tx.Commit()
}
