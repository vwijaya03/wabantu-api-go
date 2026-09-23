package tenant

import (
	"context"

	"encore.dev"

	"encore.app/wabantu/shared/tenantschema"
)

// EnsureStorefrontSchema applies storefront DDL on older tenant schemas (idempotent).
func EnsureStorefrontSchema(ctx context.Context, schemaName string) error {
	pool := DataDB.Stdlib()
	return alwaysApplyStorefrontPatch(ctx, pool, schemaName)
}

func alwaysApplyStorefrontPatch(ctx context.Context, q any, schemaName string) error {
	ready, err := tenantschema.StorefrontReady(ctx, q, schemaName)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return EnsureCloudAdminTenantDDL(ctx, schemaName)
	}
	_, err = tenantschema.Q(q).ExecContext(ctx, tenantschema.StorefrontPatchSQL)
	return err
}
