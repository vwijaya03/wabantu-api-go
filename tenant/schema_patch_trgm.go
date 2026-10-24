package tenant

import (
	"context"
	"database/sql"
	"errors"

	"encore.dev/rlog"
)

// trgmExtensionSchemaSQL resolves the schema pg_trgm is installed in (0 rows = absent).
// CREATE EXTENSION without SCHEMA installs into the first schema on search_path, which on
// a tenant connection is the tenant schema itself ("<tenant>", public), so the extension
// may live in an arbitrary (tenant) schema — never assume public / search_path.
const trgmExtensionSchemaSQL = `
SELECT n.nspname
FROM pg_extension e
JOIN pg_namespace n ON n.oid = e.extnamespace
WHERE e.extname = 'pg_trgm'`

// createTrgmExtensionSQL installs pg_trgm into public only. Never let it land in a tenant
// schema: DROP SCHEMA ... CASCADE of that tenant would drop the extension and every other
// tenant's trigram index with it.
const createTrgmExtensionSQL = `CREATE EXTENSION IF NOT EXISTS pg_trgm SCHEMA public`

// catalogTrigramIndexSQL builds the catalog trigram index DDL with the operator class
// qualified by the schema pg_trgm lives in. extSchema comes from pg_namespace and is
// always quoted with quoteIdent (never interpolated raw; it is not a tenant schema, so
// schemaNameRe does not apply). Unqualified table: runs on a tenant connection.
func catalogTrigramIndexSQL(extSchema string) string {
	return `CREATE INDEX IF NOT EXISTS idx_catalog_name_trgm
    ON business_catalog_item USING gin (name ` + quoteIdent(extSchema) + `.gin_trgm_ops)
    WHERE deleted_at IS NULL`
}

// createTrgmExtension is a hook so tests can simulate "extension absent and cannot be created".
var createTrgmExtension = func(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, createTrgmExtensionSQL)
	return err
}

// ensureCatalogTrigramIndex creates idx_catalog_name_trgm with the schema-qualified pg_trgm
// operator class. Best-effort: if pg_trgm is absent and cannot be created, or the index
// DDL fails, it logs a warning and returns nil so later patches (notably the PII columns)
// still run. Only context cancellation is returned. Idempotent (IF NOT EXISTS).
func ensureCatalogTrigramIndex(ctx context.Context, conn *sql.Conn, schemaName string) error {
	extSchema, err := trgmExtensionSchema(ctx, conn)
	if err != nil {
		return trgmSkip(ctx, schemaName, "resolve pg_trgm schema", err)
	}
	if extSchema == "" {
		if err := createTrgmExtension(ctx, conn); err != nil {
			return trgmSkip(ctx, schemaName, "pg_trgm absent and cannot be created", err)
		}
		if extSchema, err = trgmExtensionSchema(ctx, conn); err != nil || extSchema == "" {
			if err == nil {
				err = errors.New("pg_trgm not found after CREATE EXTENSION")
			}
			return trgmSkip(ctx, schemaName, "resolve pg_trgm schema", err)
		}
	}
	if _, err := conn.ExecContext(ctx, catalogTrigramIndexSQL(extSchema)); err != nil {
		return trgmSkip(ctx, schemaName, "create idx_catalog_name_trgm", err)
	}
	return nil
}

func trgmExtensionSchema(ctx context.Context, conn *sql.Conn) (string, error) {
	var schema string
	err := conn.QueryRowContext(ctx, trgmExtensionSchemaSQL).Scan(&schema)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return schema, err
}

func trgmSkip(ctx context.Context, schemaName, step string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	rlog.Warn("skip catalog trigram index (catalog search works without it, slower)",
		"schema", schemaName, "step", step, "err", err)
	return nil
}
