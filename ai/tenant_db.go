package ai

import (
	"context"
	"database/sql"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/tenant"
)

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

func (ts tenantScope) QueryRowContext(ctx context.Context, query string, args ...any) appdb.Scannable {
	return ts.q.QueryRowContext(ctx, query, args...)
}

func (ts tenantScope) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return ts.q.ExecContext(ctx, query, args...)
}

type poolQuerier struct {
	pool *sql.DB
}

func (p poolQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return appdb.ExecPool(ctx, p.pool, query, args...)
}

func (p poolQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return appdb.QueryContextPool(ctx, p.pool, query, args...)
}

func (p poolQuerier) QueryRowContext(ctx context.Context, query string, args ...any) appdb.Scannable {
	return appdb.PoolQueryRow(ctx, p.pool, query, args...)
}

func openTenantScope(ctx context.Context, schema string) (tenantScope, error) {
	if err := tenant.PrepareTenantAccess(ctx, schema); err != nil {
		return tenantScope{}, err
	}
	return tenantScope{
		q:   poolQuerier{pool: aiDB.Stdlib()},
		sch: appdb.SchemaSQL{Schema: schema},
	}, nil
}
