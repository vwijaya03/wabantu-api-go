package tenantctx

import (
	"context"
	"database/sql"
	"strings"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

// Schema returns the effective tenant schema or an error if missing (e.g. platform admin without impersonation).
func Schema(user *types.AuthUser) (string, error) {
	if user == nil || strings.TrimSpace(user.TenantSchema) == "" {
		return "", errs.Forbidden("tenant context required — pantau tenant dari konsol admin")
	}
	return user.TenantSchema, nil
}

// Conn returns a tenant DB connection with search_path set (legacy DDL / unmigrated modules).
func Conn(ctx context.Context, pool *sql.DB, user *types.AuthUser) (*sql.Conn, error) {
	schema, err := Schema(user)
	if err != nil {
		return nil, err
	}
	return appdb.TenantConn(ctx, pool, schema)
}
