package tenant

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"encore.dev"
	"encore.dev/rlog"
)

const cloudDBTenantAdmin = "db_tenant_admin"

// ensureCloudSchemaDeployGrants grants Encore Cloud deploy/migrator access to a tenant schema.
// Must run on the same connection that created the schema (encore_container) so GRANT/OWNER succeed.
func ensureCloudSchemaDeployGrants(ctx context.Context, conn *sql.Conn, schemaName string) error {
	if encore.Meta().Environment.Cloud == encore.CloudLocal {
		return nil
	}
	if !schemaNameRe.MatchString(schemaName) {
		return fmt.Errorf("invalid schema name: %q", schemaName)
	}
	qSchema := quoteIdent(schemaName)
	stmts := []string{
		fmt.Sprintf(`GRANT USAGE, CREATE ON SCHEMA %s TO %s`, qSchema, cloudDBTenantAdmin),
		fmt.Sprintf(`GRANT USAGE, CREATE ON SCHEMA %s TO encore_writer, encore_reader, encore_services`, qSchema),
		fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %s TO encore_writer, encore_services`, qSchema),
		fmt.Sprintf(`GRANT SELECT ON ALL TABLES IN SCHEMA %s TO encore_reader`, qSchema),
		fmt.Sprintf(`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %s TO encore_writer, encore_services`, qSchema),
		fmt.Sprintf(`GRANT SELECT ON ALL SEQUENCES IN SCHEMA %s TO encore_reader`, qSchema),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO encore_writer, encore_services`, qSchema),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT ON TABLES TO encore_reader`, qSchema),
		fmt.Sprintf(`ALTER SCHEMA %s OWNER TO %s`, qSchema, cloudDBTenantAdmin),
	}
	for _, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			// ALTER OWNER may fail without SET ROLE; USAGE/CREATE grants are the critical path.
			if strings.Contains(stmt, "OWNER TO") {
				rlog.Warn("cloud schema owner transfer skipped", "schema", schemaName, "err", err)
				continue
			}
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	return nil
}

func quoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

// repairAllCloudSchemaDeployGrants fixes t_* schemas after signup or restore (idempotent).
func repairAllCloudSchemaDeployGrants(ctx context.Context) {
	if encore.Meta().Environment.Cloud == encore.CloudLocal {
		return
	}
	schemas, err := ListSchemaNames(ctx)
	if err != nil {
		rlog.Warn("cloud schema grant repair: list schemas failed", "err", err)
		return
	}
	conn, err := DataDB.Stdlib().Conn(ctx)
	if err != nil {
		rlog.Warn("cloud schema grant repair: connect failed", "err", err)
		return
	}
	defer conn.Close()

	for _, schema := range schemas {
		if err := ensureCloudSchemaDeployGrants(ctx, conn, schema); err != nil {
			rlog.Warn("cloud schema grant repair failed", "schema", schema, "err", err)
		}
	}
}
