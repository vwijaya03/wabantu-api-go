package db

import (
	"context"
	"database/sql"
)

// ResetPreparedStatements clears server-side prepared statements on a tenant conn.
func ResetPreparedStatements(ctx context.Context, conn *sql.Conn) {
	if conn == nil {
		return
	}
	_, _ = conn.ExecContext(ctx, "DEALLOCATE ALL")
}

// QueryRowContextRetry runs QueryRowContext+Scan and retries once after DEALLOCATE ALL
// when pgx reports a stale prepared statement (08P01).
func QueryRowContextRetry(ctx context.Context, conn *sql.Conn, dest []any, query string, args ...any) error {
	err := conn.QueryRowContext(ctx, query, args...).Scan(dest...)
	if !IsStalePreparedStatement(err) {
		return err
	}
	ResetPreparedStatements(ctx, conn)
	return conn.QueryRowContext(ctx, query, args...).Scan(dest...)
}
