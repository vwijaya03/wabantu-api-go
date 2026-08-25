package ai

import (
	"context"
	"database/sql"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/tenant"
)

// tenantScopedQuerier runs tenant-scoped SQL with explicit schema qualification via T().
type tenantScopedQuerier interface {
	tenantQuerier
	T(table string) string
}

type tenantScope struct {
	q   tenantQuerier
	sch appdb.SchemaSQL
}

func (ts tenantScope) T(table string) string {
	return ts.sch.T(table)
}

func (ts tenantScope) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return ts.q.QueryContext(ctx, query, args...)
}

func (ts tenantScope) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return ts.q.QueryRowContext(ctx, query, args...)
}

func (ts tenantScope) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return ts.q.ExecContext(ctx, query, args...)
}

func openTenantScope(ctx context.Context, schema string) (tenantScope, error) {
	if err := tenant.PrepareTenantAccess(ctx, schema); err != nil {
		return tenantScope{}, err
	}
	return tenantScope{
		q:   aiDB.Stdlib(),
		sch: appdb.SchemaSQL{Schema: schema},
	}, nil
}
