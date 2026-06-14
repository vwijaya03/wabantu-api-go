package inventory

import (
	"context"
	"database/sql"

	"encore.dev"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/tenantschema"
)

// ensureInventoryModuleReady verifies inventory DDL is complete before reads/writes.
// On Encore Cloud, ALTER on admin-owned inv_* tables must be applied via
// ./scripts/apply-inventory-schema-cloud.sh (app role cannot ALTER).
func ensureInventoryModuleReady(ctx context.Context, conn *sql.Conn) error {
	ready, err := tenantschema.InventoryModuleReady(ctx, conn)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	if ready {
		return nil
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return appErrs.Internal(
			"schema inventory belum lengkap di cloud — jalankan ./scripts/apply-inventory-schema-cloud.sh " +
				encore.Meta().Environment.Name,
		)
	}
	if _, err := conn.ExecContext(ctx, tenantschema.InventorySchemaSQL); err != nil {
		return appErrs.Internal(err.Error())
	}
	return nil
}
