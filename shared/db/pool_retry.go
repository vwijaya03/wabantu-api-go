package db

import (
	"context"
	"database/sql"
)

const poolStaleStmtMaxAttempts = 5

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
	if err == nil || !IsStalePreparedStatement(err) {
		return err
	}
	return scanOnDedicatedConns(r.ctx, r.pool, r.query, r.args, dest...)
}

func scanOnDedicatedConns(ctx context.Context, pool *sql.DB, query string, args []any, dest ...any) error {
	var lastErr error
	for i := 0; i < poolStaleStmtMaxAttempts; i++ {
		conn, cerr := pool.Conn(ctx)
		if cerr != nil {
			return lastErr
		}
		lastErr = func() error {
			defer conn.Close()
			ResetPreparedStatements(ctx, conn)
			return conn.QueryRowContext(ctx, query, args...).Scan(dest...)
		}()
		if lastErr == nil || !IsStalePreparedStatement(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

// PoolQueryRow returns a row that retries on stale pgx prepared statements (08P01).
func PoolQueryRow(ctx context.Context, pool *sql.DB, query string, args ...any) Scannable {
	return retryRow{pool: pool, ctx: ctx, query: query, args: args}
}

// ScanRowPool runs QueryRow+Scan with retry after DEALLOCATE ALL on pooled connections.
func ScanRowPool(ctx context.Context, pool *sql.DB, query string, args []any, dest ...any) error {
	return PoolQueryRow(ctx, pool, query, args...).Scan(dest...)
}

// ExecPool runs Exec with retry after DEALLOCATE ALL on pooled connections.
func ExecPool(ctx context.Context, pool *sql.DB, query string, args ...any) (sql.Result, error) {
	res, err := pool.ExecContext(ctx, query, args...)
	if err == nil || !IsStalePreparedStatement(err) {
		return res, err
	}
	return execOnDedicatedConns(ctx, pool, query, args...)
}

func execOnDedicatedConns(ctx context.Context, pool *sql.DB, query string, args ...any) (sql.Result, error) {
	var lastRes sql.Result
	var lastErr error
	for i := 0; i < poolStaleStmtMaxAttempts; i++ {
		conn, cerr := pool.Conn(ctx)
		if cerr != nil {
			return lastRes, lastErr
		}
		lastRes, lastErr = func() (sql.Result, error) {
			defer conn.Close()
			ResetPreparedStatements(ctx, conn)
			return conn.ExecContext(ctx, query, args...)
		}()
		if lastErr == nil || !IsStalePreparedStatement(lastErr) {
			return lastRes, lastErr
		}
	}
	return lastRes, lastErr
}

// QueryContextPool runs Query with retry after DEALLOCATE ALL on pooled connections.
func QueryContextPool(ctx context.Context, pool *sql.DB, query string, args ...any) (*sql.Rows, error) {
	rows, err := pool.QueryContext(ctx, query, args...)
	if err == nil || !IsStalePreparedStatement(err) {
		return rows, err
	}
	return queryOnDedicatedConns(ctx, pool, query, args...)
}

func queryOnDedicatedConns(ctx context.Context, pool *sql.DB, query string, args ...any) (*sql.Rows, error) {
	var lastRows *sql.Rows
	var lastErr error
	for i := 0; i < poolStaleStmtMaxAttempts; i++ {
		conn, cerr := pool.Conn(ctx)
		if cerr != nil {
			return lastRows, lastErr
		}
		lastRows, lastErr = func() (*sql.Rows, error) {
			defer conn.Close()
			ResetPreparedStatements(ctx, conn)
			return conn.QueryContext(ctx, query, args...)
		}()
		if lastErr == nil || !IsStalePreparedStatement(lastErr) {
			return lastRows, lastErr
		}
	}
	return lastRows, lastErr
}
