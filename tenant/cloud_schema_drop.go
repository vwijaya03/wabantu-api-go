package tenant

import (
	"context"
	"fmt"
	"strings"

	"encore.dev"
)

// DropTenantSchema permanently removes a tenant schema (destructive).
func DropTenantSchema(ctx context.Context, schemaName string) error {
	if !schemaNameRe.MatchString(schemaName) {
		return fmt.Errorf("invalid schema name: %q", schemaName)
	}

	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		if err := dropTenantSchemaViaFunction(ctx, schemaName); err == nil {
			return nil
		} else if !isMissingDropTenantSchemaFunction(err) {
			return err
		}
		return dropTenantSchemaViaSetRole(ctx, schemaName)
	}
	return dropTenantSchemaDirect(ctx, schemaName)
}

func dropTenantSchemaViaFunction(ctx context.Context, schemaName string) error {
	_, err := DataDB.Stdlib().ExecContext(ctx, `SELECT public.drop_tenant_schema($1)`, schemaName)
	if err != nil {
		return fmt.Errorf(
			"drop tenant schema function (jalankan ./scripts/fix-cloud-db-grants.sh %s): %w",
			encore.Meta().Environment.Name,
			err,
		)
	}
	return nil
}

func dropTenantSchemaViaSetRole(ctx context.Context, schemaName string) error {
	tx, err := DataDB.Stdlib().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin drop schema tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL ROLE %s", cloudDBTenantAdmin)); err != nil {
		var currentUser string
		_ = tx.QueryRowContext(ctx, `SELECT current_user`).Scan(&currentUser)
		return fmt.Errorf(
			"set tenant admin role as %s (jalankan ./scripts/fix-cloud-db-grants.sh %s): %w",
			currentUser,
			encore.Meta().Environment.Name,
			err,
		)
	}

	qSchema := quoteIdent(schemaName)
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, qSchema)); err != nil {
		return fmt.Errorf("drop schema %s: %w", schemaName, err)
	}
	return tx.Commit()
}

func dropTenantSchemaDirect(ctx context.Context, schemaName string) error {
	qSchema := quoteIdent(schemaName)
	_, err := DataDB.Stdlib().ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, qSchema))
	if err != nil {
		return fmt.Errorf("drop schema %s: %w", schemaName, err)
	}
	return nil
}

func isMissingDropTenantSchemaFunction(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "drop_tenant_schema") &&
		(strings.Contains(msg, "does not exist") || strings.Contains(msg, "42883"))
}
