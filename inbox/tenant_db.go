package inbox

import (
	"context"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/tenant"
)

func openTenantScope(ctx context.Context, schema string) (appdb.TenantScope, error) {
	if err := tenant.PrepareTenantAccess(ctx, schema); err != nil {
		return appdb.TenantScope{}, err
	}
	return appdb.OpenTenantScope(db.Stdlib(), schema), nil
}
