package db

import (
	"context"
	"database/sql"
)

// stdQuerier adapts *sql.DB, *sql.Conn, and *sql.Tx to TenantQuerier.
type stdQuerier struct {
	q interface {
		ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
		QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	}
}

func (s stdQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.q.ExecContext(ctx, query, args...)
}

func (s stdQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.q.QueryContext(ctx, query, args...)
}

func (s stdQuerier) QueryRowContext(ctx context.Context, query string, args ...any) Scannable {
	return s.q.QueryRowContext(ctx, query, args...)
}

func AsTenantQuerier(q interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}) TenantQuerier {
	if pool, ok := q.(*sql.DB); ok {
		return poolRetryQuerier{pool: pool}
	}
	if conn, ok := q.(*sql.Conn); ok {
		return connRetryQuerier{conn: conn}
	}
	return stdQuerier{q: q}
}

type poolRetryQuerier struct {
	pool *sql.DB
}

func (p poolRetryQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return ExecPool(ctx, p.pool, query, args...)
}

func (p poolRetryQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := p.pool.QueryContext(ctx, query, args...)
	if !IsStalePreparedStatement(err) {
		return rows, err
	}
	conn, cerr := p.pool.Conn(ctx)
	if cerr != nil {
		return rows, err
	}
	defer conn.Close()
	ResetPreparedStatements(ctx, conn)
	return conn.QueryContext(ctx, query, args...)
}

func (p poolRetryQuerier) QueryRowContext(ctx context.Context, query string, args ...any) Scannable {
	return PoolQueryRow(ctx, p.pool, query, args...)
}

type connRetryQuerier struct {
	conn *sql.Conn
}

func (c connRetryQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.conn.ExecContext(ctx, query, args...)
}

func (c connRetryQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.conn.QueryContext(ctx, query, args...)
}

func (c connRetryQuerier) QueryRowContext(ctx context.Context, query string, args ...any) Scannable {
	return connRetryRow{conn: c.conn, ctx: ctx, query: query, args: args}
}

type connRetryRow struct {
	conn  *sql.Conn
	ctx   context.Context
	query string
	args  []any
}

func (r connRetryRow) Scan(dest ...any) error {
	return QueryRowContextRetry(r.ctx, r.conn, r.query, r.args, dest...)
}

// CoerceTenantQuerier wraps *sql.DB / *sql.Conn with 08P01 retry, or returns q as-is.
func CoerceTenantQuerier(q TenantQuerier) TenantQuerier {
	switch v := any(q).(type) {
	case *sql.DB:
		return AsTenantQuerier(v)
	case *sql.Conn:
		return connRetryQuerier{conn: v}
	default:
		return q
	}
}
