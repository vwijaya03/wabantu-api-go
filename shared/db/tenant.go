package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

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
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+QuoteIdent(schema)+", public"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set search_path: %w", err)
	}
	return conn, nil
}

// CloseTenantConn resets session state before returning the connection to the pool.
func CloseTenantConn(conn *sql.Conn) {
	if conn == nil {
		return
	}
	_, _ = conn.ExecContext(context.Background(), "RESET search_path")
	_, _ = conn.ExecContext(context.Background(), "RESET ROLE")
	conn.Close()
}
