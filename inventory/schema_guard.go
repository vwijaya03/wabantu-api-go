package inventory

import (
	"context"
	"database/sql"

	"encore.dev"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/tenantschema"
	"encore.app/wabantu/tenant"
)

// ensureInventoryModuleSchema verifies inventory DDL on a dedicated connection.
// Use before read-heavy handlers so runtime DDL does not share a conn with list queries.
func ensureInventoryModuleSchema(ctx context.Context, schemaName string) error {
	if isInventorySchemaReadyCached(schemaName) {
		return nil
	}
	conn, err := tenant.TenantConn(ctx, schemaName)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := ensureInventoryModuleReady(ctx, conn); err != nil {
		return err
	}
	markInventorySchemaReady(schemaName)
	return nil
}

// ensureInventoryModuleReady verifies inventory DDL is complete before reads/writes.
// On Encore Cloud, inv_* DDL is applied via db_tenant_admin (EnsureCloudAdminTenantDDL).
func ensureInventoryModuleReady(ctx context.Context, conn *sql.Conn) error {
	ready, err := tenantschema.InventoryModuleReady(ctx, conn)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	if ready {
		return nil
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		var schemaName string
		if err := conn.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schemaName); err != nil {
			return appErrs.Internal(err.Error())
		}
		if err := tenant.EnsureCloudAdminTenantDDL(ctx, schemaName); err != nil {
			return appErrs.Internal(err.Error())
		}
		ready, err = tenantschema.InventoryModuleReady(ctx, conn)
		if err != nil {
			return appErrs.Internal(err.Error())
		}
		if !ready {
			return appErrs.Internal("schema inventory masih belum lengkap setelah cloud DDL")
		}
		return nil
	}
	if _, err := conn.ExecContext(ctx, tenantschema.InventorySchemaSQL); err != nil {
		return appErrs.Internal(err.Error())
	}
	return nil
}
