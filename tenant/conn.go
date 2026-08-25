package tenant

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	appdb "encore.app/wabantu/shared/db"
)

// PrepareTenantAccess validates the schema name and schedules lazy migration.
// Use for DML that uses schema-qualified table names (no SET search_path).
func PrepareTenantAccess(ctx context.Context, schemaName string) error {
	if err := ValidateTenantSchemaName(schemaName); err != nil {
		return err
	}
	go maybeLazyMigrateFromSchema(context.Background(), schemaName)
	return nil
}

// tenantSchemaFromConn resolves tenant schema from a connection with search_path set.
func tenantSchemaFromConn(ctx context.Context, conn *sql.Conn) (string, error) {
	return SchemaFromConn(ctx, conn)
}

func CloseTenantConn(conn *sql.Conn) {
	appdb.CloseTenantConn(conn)
}

// ValidateTenantSchemaName rejects empty or public schema names used by mistake when
// current_schema() falls through because the tenant schema does not exist yet.
func ValidateTenantSchemaName(schemaName string) error {
	if !schemaNameRe.MatchString(schemaName) {
		return fmt.Errorf("invalid schema name: %q", schemaName)
	}
	if schemaName == "public" {
		return fmt.Errorf("tenant schema %q is not provisioned", schemaName)
	}
	return nil
}

// EnsureTenantSchemaProvisioned creates base tenant DDL when core tables are missing.
func EnsureTenantSchemaProvisioned(ctx context.Context, schemaName string) error {
	if err := ValidateTenantSchemaName(schemaName); err != nil {
		return err
	}
	provisioned, err := tenantSchemaBaseProvisioned(ctx, schemaName)
	if err != nil {
		return err
	}
	if provisioned {
		return nil
	}
	return RunTenantDDL(ctx, schemaName)
}

// SchemaFromConn returns the first schema in the connection search_path.
func SchemaFromConn(ctx context.Context, conn *sql.Conn) (string, error) {
	var raw string
	if err := conn.QueryRowContext(ctx, `SELECT current_setting('search_path')`).Scan(&raw); err != nil {
		return "", fmt.Errorf("read search_path: %w", err)
	}
	part := strings.TrimSpace(strings.SplitN(raw, ",", 2)[0])
	part = strings.Trim(part, `"`)
	if err := ValidateTenantSchemaName(part); err != nil {
		return "", err
	}
	return part, nil
}
