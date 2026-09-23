package tenant

import (
	"context"

	"encore.dev"

	"encore.app/wabantu/shared/tenantschema"
)

// EnsureChatWidgetSchema applies web chat widget DDL on older tenant schemas (idempotent).
func EnsureChatWidgetSchema(ctx context.Context, schemaName string) error {
	pool := DataDB.Stdlib()
	return alwaysApplyChatWidgetPatch(ctx, pool, schemaName)
}

func alwaysApplyChatWidgetPatch(ctx context.Context, q any, schemaName string) error {
	ready, err := tenantschema.ChatWidgetReady(ctx, q, schemaName)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return EnsureCloudAdminTenantDDL(ctx, schemaName)
	}
	_, err = tenantschema.Q(q).ExecContext(ctx, tenantschema.ChatWidgetPatchSQL)
	return err
}
