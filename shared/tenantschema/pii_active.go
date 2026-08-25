package tenantschema

import (
	"context"
	"database/sql"
)

func currentSchema(ctx context.Context, db Querier) string {
	var schema string
	_ = db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema)
	return schema
}

func contactPIIActiveUncached(ctx context.Context, db Querier, schema string) (bool, error) {
	ready, err := ColumnExists(ctx, db, schema, "contact", "phone_number_idx")
	if err == nil {
		storeContactPII(schema, ready)
	}
	return ready, err
}

func leadPIIActiveUncached(ctx context.Context, db Querier, schema string) (bool, error) {
	ready, err := ColumnExists(ctx, db, schema, "lead", "phone_number_idx")
	if err == nil {
		storeLeadPII(schema, ready)
	}
	return ready, err
}

// ContactPIIActive reports whether contact encrypted columns exist (cached per schema).
func ContactPIIActive(ctx context.Context, db any, schema string) (bool, error) {
	q := Q(db)
	if schema == "" {
		schema = currentSchema(ctx, q)
	}
	if active, ok := cachedContactPII(schema); ok {
		return active, nil
	}
	return contactPIIActiveUncached(ctx, q, schema)
}

// ContactPIIActiveConn is ContactPIIActive for *sql.Conn.
func ContactPIIActiveConn(ctx context.Context, conn *sql.Conn, schema string) (bool, error) {
	return ContactPIIActive(ctx, conn, schema)
}

// ContactPIIActiveDB is ContactPIIActive for *sql.DB.
func ContactPIIActiveDB(ctx context.Context, db *sql.DB, schema string) (bool, error) {
	return ContactPIIActive(ctx, db, schema)
}

// LeadPIIActive reports whether lead encrypted columns exist (cached per schema).
func LeadPIIActive(ctx context.Context, db any, schema string) (bool, error) {
	q := Q(db)
	if schema == "" {
		schema = currentSchema(ctx, q)
	}
	if active, ok := cachedLeadPII(schema); ok {
		return active, nil
	}
	return leadPIIActiveUncached(ctx, q, schema)
}

// LeadPIIActiveDB is LeadPIIActive for *sql.DB.
func LeadPIIActiveDB(ctx context.Context, db *sql.DB, schema string) (bool, error) {
	return LeadPIIActive(ctx, db, schema)
}

// TableColumnExists checks a column in an explicit Postgres schema.
func TableColumnExists(ctx context.Context, db any, schema, table, column string) (bool, error) {
	q := Q(db)
	const query = `
		SELECT EXISTS (
		  SELECT 1 FROM information_schema.columns
		  WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
		)`
	var exists bool
	err := q.QueryRowContext(ctx, query, schema, table, column).Scan(&exists)
	return exists, err
}

// UseBlindIndexSearch is true when encryption key is configured and blind-index columns exist.
func UseBlindIndexSearch(encKey string, piiColumnsReady bool) bool {
	if !piiColumnsReady {
		return false
	}
	// ValidateKey imported would create cycle if in pii package; callers pass key validity.
	return len(encKey) >= 32
}
