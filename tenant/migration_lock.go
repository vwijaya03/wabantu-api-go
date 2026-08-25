package tenant

import (
	"context"
	"database/sql"
	"fmt"

	"encore.app/wabantu/system"
)

// withSchemaMigrationLock serializes schema migrations per tenant schema across workers.
func withSchemaMigrationLock(ctx context.Context, schemaName string, fn func() error) error {
	conn, err := DataDB.Stdlib().Conn(ctx)
	if err != nil {
		return fmt.Errorf("migration lock conn: %w", err)
	}
	defer conn.Close()

	var acquired bool
	if err := conn.QueryRowContext(ctx,
		`SELECT pg_try_advisory_lock(hashtext($1))`, schemaName,
	).Scan(&acquired); err != nil {
		return fmt.Errorf("migration lock acquire: %w", err)
	}
	if !acquired {
		return errSchemaMigrationBusy
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(),
			`SELECT pg_advisory_unlock(hashtext($1))`, schemaName)
	}()

	return fn()
}

// schemaMigrationJobActive reports queued/running admin migration jobs for a schema.
func schemaMigrationJobActive(ctx context.Context, schemaName string) (bool, error) {
	var active bool
	err := system.DB.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM tenant_schema_migration_job_item
		  WHERE schema_name = $1
		    AND status = ANY($2::text[])
		)`, schemaName, []string{migrationItemStatusQueued, migrationItemStatusRunning},
	).Scan(&active)
	return active, err
}

// tenantMigrationConn is like TenantConn but does not enqueue lazy migration (DDL/migration paths only).
func tenantMigrationConn(ctx context.Context, schemaName string) (*sql.Conn, error) {
	if !schemaNameRe.MatchString(schemaName) {
		return nil, fmt.Errorf("invalid schema name: %q", schemaName)
	}
	conn, err := DataDB.Stdlib().Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire conn: %w", err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`SET search_path TO "%s", public`, schemaName)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set search_path: %w", err)
	}
	return conn, nil
}
