package events

import (
	"context"

	"encore.dev/storage/sqldb"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/tenant"
)

var eventsDB = sqldb.Named("tenant")

// tenantScope is schema-qualified tenant DML with pool 08P01 retry (no SET search_path).
type tenantScope = appdb.TenantScope

func openTenant(ctx context.Context, schema string) (tenantScope, error) {
	if err := tenant.PrepareTenantAccess(ctx, schema); err != nil {
		return tenantScope{}, err
	}
	if err := ensureEventsSchema(ctx, schema); err != nil {
		return tenantScope{}, err
	}
	return appdb.OpenTenantScope(eventsDB.Stdlib(), schema), nil
}
