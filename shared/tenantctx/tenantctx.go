package tenantctx

import (
	"context"
	"database/sql"
	"strings"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

// Conn returns a tenant DB connection or an error if the user has no effective tenant schema
// (e.g. platform admin on the console without impersonation).
func Conn(ctx context.Context, pool *sql.DB, user *types.AuthUser) (*sql.Conn, error) {
	if user == nil || strings.TrimSpace(user.TenantSchema) == "" {
		return nil, errs.Forbidden("tenant context required — pantau tenant dari konsol admin")
	}
	return appdb.TenantConn(ctx, pool, user.TenantSchema)
}
