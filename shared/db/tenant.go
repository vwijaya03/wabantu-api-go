package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsStalePreparedStatement reports pgx cached-plan errors after search_path changes.
func IsStalePreparedStatement(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "08P01"
	}
	return strings.Contains(err.Error(), "prepared statement") ||
		strings.Contains(err.Error(), "SQLSTATE 08P01")
}

// QuoteIdent safely quotes a SQL identifier to prevent injection.
func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// TenantConn returns a dedicated *sql.Conn with search_path set to the
// given tenant schema.  Caller MUST defer CloseTenantConn(conn).
func TenantConn(ctx context.Context, pool *sql.DB, schema string) (*sql.Conn, error) {
	conn, err := pool.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("get connection: %w", err)
	}
	// Pool reuse can leave server-side prepared statements from a prior tenant session.
	_, _ = conn.ExecContext(ctx, "DEALLOCATE ALL")
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+QuoteIdent(schema)+", public"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set search_path: %w", err)
	}
	return conn, nil
}

// CloseTenantConn resets session state before returning the connection to the pool.
// DEALLOCATE ALL is required because pgx caches server-side prepared statements per
// connection; RESET search_path alone leaves stale plans that can reference the
// wrong tenant schema (or dropped OIDs) after the path changes.
func CloseTenantConn(conn *sql.Conn) {
	if conn == nil {
		return
	}
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, "DEALLOCATE ALL"); err != nil {
		// Connection already broken (e.g. 08P01); discard instead of resetting session.
		conn.Close()
		return
	}
	_, _ = conn.ExecContext(ctx, "RESET search_path")
	_, _ = conn.ExecContext(ctx, "RESET ROLE")
	conn.Close()
}
