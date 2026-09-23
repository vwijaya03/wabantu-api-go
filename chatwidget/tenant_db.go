package chatwidget

import (
	"context"

	"encore.dev/storage/sqldb"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/tenant"
)

var tenantDB = sqldb.Named("tenant")

func openTenant(ctx context.Context, schema string) (appdb.TenantScope, error) {
	if err := tenant.PrepareTenantAccess(ctx, schema); err != nil {
		return appdb.TenantScope{}, err
	}
	if err := tenant.EnsureChatWidgetSchema(ctx, schema); err != nil {
		return appdb.TenantScope{}, err
	}
	return appdb.OpenTenantScope(tenantDB.Stdlib(), schema), nil
}
