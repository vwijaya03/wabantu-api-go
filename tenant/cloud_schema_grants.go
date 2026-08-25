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

// Runtime roles that execute Encore service SQL; need membership to SET ROLE db_tenant_admin.
var cloudRuntimeRoles = []string{"encore_services", "encore_writer"}

// ensureCloudSchemaDeployGrants grants Encore Cloud deploy/migrator access to a tenant schema.
// Must run on the same connection that created the schema (encore_container) so GRANT/OWNER succeed.
//
// Tables created by RunTenantDDL are owned by encore_container_*. Encore deploy dynamic grants run
// as encore_admin_* (member of db_tenant_admin). If tables stay owned by encore_container, deploy
// fails with: permission denied for table business_profile.
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
	if err := transferCloudSchemaObjectOwners(ctx, conn, schemaName); err != nil {
		rlog.Warn("cloud table owner transfer skipped", "schema", schemaName, "err", err)
	}
	ensureCloudRuntimeRoleMembership(ctx, conn)
	return nil
}

// transferCloudSchemaObjectOwners moves tables/sequences/views in schema to db_tenant_admin so
// encore_admin (member of db_tenant_admin) can execute Encore Cloud dynamic grants on deploy.
func transferCloudSchemaObjectOwners(ctx context.Context, conn *sql.Conn, schemaName string) error {
	// Only transfer objects we currently own — safe after RunTenantDDL on the creating connection.
	stmt := fmt.Sprintf(`
DO $do$
DECLARE
  r record;
  target text := %s;
  sch text := %s;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = target) THEN
    RETURN;
  END IF;
  FOR r IN
    SELECT c.relname,
           CASE WHEN c.relkind = 'S' THEN 'SEQUENCE' ELSE 'TABLE' END AS kind
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = sch
      AND c.relkind IN ('r', 'p', 'S', 'v', 'm')
      AND pg_get_userbyid(c.relowner) = current_user
  LOOP
    BEGIN
      EXECUTE format('ALTER %%s %%I.%%I OWNER TO %%I', r.kind, sch, r.relname, target);
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'skip object owner %%.%%: %%', sch, r.relname, SQLERRM;
    END;
  END LOOP;
END
$do$`, quoteLiteral(cloudDBTenantAdmin), quoteLiteral(schemaName))
	_, err := conn.ExecContext(ctx, stmt)
	return err
}

func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ensureCloudRuntimeRoleMembership lets encore_services/writer assume db_tenant_admin for DDL
// such as DROP SCHEMA from super-admin APIs. Idempotent; best-effort on connections without grant privilege.
func ensureCloudRuntimeRoleMembership(ctx context.Context, conn *sql.Conn) {
	if encore.Meta().Environment.Cloud == encore.CloudLocal {
		return
	}
	for _, role := range cloudRuntimeRoles {
		stmt := fmt.Sprintf("GRANT %s TO %s", cloudDBTenantAdmin, role)
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			rlog.Warn("cloud runtime role membership grant skipped", "grantee", role, "err", err)
		}
	}
}

func quoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

// repairAllCloudSchemaDeployGrants fixes t_* schemas after signup or restore (idempotent).
func repairAllCloudSchemaDeployGrants(ctx context.Context) {
	if encore.Meta().Environment.Cloud == encore.CloudLocal {
		return
	}
	if _, err := repairAllTenantSchemaDeployGrants(ctx); err != nil {
		rlog.Warn("cloud schema grant repair failed", "err", err)
	}
}
