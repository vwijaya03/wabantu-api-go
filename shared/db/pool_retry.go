package db

import (
	"context"
	"database/sql"
)

// Scannable is satisfied by *sql.Row and pool retry rows.
type Scannable interface {
	Scan(dest ...any) error
}

type retryRow struct {
	pool  *sql.DB
	ctx   context.Context
	query string
	args  []any
}

func (r retryRow) Scan(dest ...any) error {
	err := r.pool.QueryRowContext(r.ctx, r.query, r.args...).Scan(dest...)
	if !IsStalePreparedStatement(err) {
		return err
	}
	conn, cerr := r.pool.Conn(r.ctx)
	if cerr != nil {
		return err
	}
	defer conn.Close()
	ResetPreparedStatements(r.ctx, conn)
	return conn.QueryRowContext(r.ctx, r.query, r.args...).Scan(dest...)
}

// PoolQueryRow returns a row that retries once on stale pgx prepared statements (08P01).
func PoolQueryRow(ctx context.Context, pool *sql.DB, query string, args ...any) Scannable {
	return retryRow{pool: pool, ctx: ctx, query: query, args: args}
}

// ScanRowPool runs QueryRow+Scan with one retry after DEALLOCATE ALL on pooled connections.
func ScanRowPool(ctx context.Context, pool *sql.DB, query string, args []any, dest ...any) error {
	return PoolQueryRow(ctx, pool, query, args...).Scan(dest...)
}

// ExecPool runs Exec with one retry after DEALLOCATE ALL on pooled connections.
func ExecPool(ctx context.Context, pool *sql.DB, query string, args ...any) (sql.Result, error) {
	res, err := pool.ExecContext(ctx, query, args...)
	if !IsStalePreparedStatement(err) {
		return res, err
	}
	conn, cerr := pool.Conn(ctx)
	if cerr != nil {
		return res, err
	}
	defer conn.Close()
	ResetPreparedStatements(ctx, conn)
	return conn.ExecContext(ctx, query, args...)
}
