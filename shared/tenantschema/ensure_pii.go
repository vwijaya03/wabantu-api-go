package tenantschema

import (
	"context"
	"database/sql"
)

type columnChecker interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func columnExistsChecker(ctx context.Context, db columnChecker, table, column string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM information_schema.columns
		  WHERE table_schema = current_schema()
		    AND table_name = $1 AND column_name = $2
		)`, table, column).Scan(&exists)
	return exists, err
}

// ContactPIIReady reports whether contact PII columns exist (uncached).
func ContactPIIReady(ctx context.Context, conn *sql.Conn) (bool, error) {
	return columnExists(ctx, conn, "contact", "phone_number_idx")
}

// ContactPIIReadyDB is ContactPIIReady for *sql.DB connections.
func ContactPIIReadyDB(ctx context.Context, db *sql.DB) (bool, error) {
	return columnExistsChecker(ctx, db, "contact", "phone_number_idx")
}

// LeadPIIReadyDB reports whether lead PII columns exist (uncached).
func LeadPIIReadyDB(ctx context.Context, db *sql.DB) (bool, error) {
	return columnExistsChecker(ctx, db, "lead", "phone_number_idx")
}

// PIIPatchRunner applies PII DDL when columns are missing (injected to avoid import cycles).
type PIIPatchRunner func(ctx context.Context, conn *sql.Conn) error

// EnsureContactPII applies idempotent PII DDL/constraints and reports if encrypted writes are safe.
func EnsureContactPII(ctx context.Context, conn *sql.Conn, schema string, patch PIIPatchRunner) bool {
	if schema == "" {
		schema = currentSchema(ctx, conn)
	}
	wasActive, _ := ContactPIIActiveConn(ctx, conn, schema)
	if patch != nil {
		_ = patch(ctx, conn)
		if !wasActive {
			InvalidateContactPIICache(schema)
		}
	}
	active, err := contactPIIActiveUncached(ctx, conn, schema)
	if active {
		MarkContactPIIActive(schema)
	}
	return active && err == nil
}
