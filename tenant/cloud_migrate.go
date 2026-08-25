package tenant

import (
	"context"
	"fmt"
	"strings"

	"encore.app/wabantu/shared/tenantschema"
	"encore.app/wabantu/system"
	"encore.dev"
	"encore.dev/rlog"
)

// CloudMigrationPrepResult summarizes global cloud repair before/after tenant migrations.
type CloudMigrationPrepResult struct {
	OrphansPruned   []string `json:"orphansPruned,omitempty"`
	GrantsRepaired  int      `json:"grantsRepaired,omitempty"`
	DeployReady     bool     `json:"deployReady"`
	DeployBlockers  []string `json:"deployBlockers,omitempty"`
	RepairFnMissing bool     `json:"repairFnMissing,omitempty"`
}

func isEncoreCloud() bool {
	return encore.Meta().Environment.Cloud != encore.CloudLocal
}

// RunCloudMigrationPrep prunes orphan t_* schemas and repairs deploy grants on all registered tenants.
// Safe to call before migrate-tenant-schemas (idempotent).
func RunCloudMigrationPrep(ctx context.Context) (*CloudMigrationPrepResult, error) {
	if !isEncoreCloud() {
		return &CloudMigrationPrepResult{DeployReady: true}, nil
	}

	out := &CloudMigrationPrepResult{}
	fnOK, err := repairTenantSchemaGrantsFunctionExists(ctx)
	if err != nil {
		return nil, err
	}
	out.RepairFnMissing = !fnOK
	if !fnOK {
		rlog.Warn("repair_tenant_schema_grants() missing — deploy migration 4/5 belum terpasang")
	}

	pruned, err := pruneOrphanTenantSchemas(ctx)
	if err != nil {
		return nil, err
	}
	out.OrphansPruned = pruned

	repaired, err := repairAllTenantSchemaDeployGrants(ctx)
	if err != nil {
		return nil, err
	}
	out.GrantsRepaired = repaired

	blockers, err := listCloudDeployBlockers(ctx)
	if err != nil {
		return nil, err
	}
	out.DeployBlockers = blockers
	out.DeployReady = len(blockers) == 0
	return out, nil
}

// RepairTenantSchemaDeployGrants fixes schema/table owners + GRANTs for one tenant schema.
func RepairTenantSchemaDeployGrants(ctx context.Context, schemaName string) error {
	if !isEncoreCloud() {
		return nil
	}
	if !schemaNameRe.MatchString(schemaName) {
		return fmt.Errorf("invalid schema name: %q", schemaName)
	}

	if ok, err := repairTenantSchemaGrantsFunctionExists(ctx); err != nil {
		return err
	} else if ok {
		_, err := DataDB.Stdlib().ExecContext(ctx,
			`SELECT public.repair_tenant_schema_grants($1)`, schemaName)
		return err
	}

	conn, err := DataDB.Stdlib().Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Close()
	return ensureCloudSchemaDeployGrants(ctx, conn, schemaName)
}

// repairAllTenantSchemaDeployGrants repairs every t_* schema (registered + orphan leftovers).
func repairAllTenantSchemaDeployGrants(ctx context.Context) (int, error) {
	if !isEncoreCloud() {
		return 0, nil
	}
	schemas, err := ListSchemaNames(ctx)
	if err != nil {
		return 0, err
	}
	repaired := 0
	for _, schema := range schemas {
		if err := RepairTenantSchemaDeployGrants(ctx, schema); err != nil {
			rlog.Warn("cloud grant repair failed", "schema", schema, "err", err)
			continue
		}
		repaired++
	}
	return repaired, nil
}

func repairTenantSchemaGrantsFunctionExists(ctx context.Context) (bool, error) {
	var exists bool
	err := DataDB.Stdlib().QueryRowContext(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM pg_proc p
		  JOIN pg_namespace n ON n.oid = p.pronamespace
		  WHERE n.nspname = 'public' AND p.proname = 'repair_tenant_schema_grants'
		)`).Scan(&exists)
	return exists, err
}

func pruneOrphanTenantSchemas(ctx context.Context) ([]string, error) {
	orphans, err := listOrphanTenantSchemas(ctx)
	if err != nil {
		return nil, err
	}
	pruned := make([]string, 0, len(orphans))
	for _, schema := range orphans {
		if err := DropTenantSchema(ctx, schema); err != nil {
			return pruned, fmt.Errorf("prune orphan schema %s: %w", schema, err)
		}
		pruned = append(pruned, schema)
		rlog.Info("pruned orphan tenant schema", "schema", schema)
	}
	return pruned, nil
}

func listOrphanTenantSchemas(ctx context.Context) ([]string, error) {
	all, err := ListSchemaNames(ctx)
	if err != nil {
		return nil, err
	}
	registered, err := listRegisteredSchemaNames(ctx)
	if err != nil {
		return nil, err
	}
	regSet := make(map[string]struct{}, len(registered))
	for _, s := range registered {
		regSet[s] = struct{}{}
	}
	var orphans []string
	for _, s := range all {
		if _, ok := regSet[s]; !ok {
			orphans = append(orphans, s)
		}
	}
	return orphans, nil
}

func listRegisteredSchemaNames(ctx context.Context) ([]string, error) {
	rows, err := system.DB.Query(ctx, `
		SELECT tc.schema_name
		FROM tenant_company tc
		JOIN tenant t ON t.id = tc.tenant_id
		WHERE t.deleted_at IS NULL
		  AND tc.schema_name IS NOT NULL AND tc.schema_name <> ''
		ORDER BY tc.schema_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func listCloudDeployBlockers(ctx context.Context) ([]string, error) {
	if orphans, err := listOrphanTenantSchemas(ctx); err != nil {
		return nil, err
	} else if len(orphans) > 0 {
		return []string{fmt.Sprintf("orphan schemas: %s", strings.Join(orphans, ", "))}, nil
	}

	rows, err := DataDB.Stdlib().QueryContext(ctx, `
		SELECT n.nspname || '.' || c.relname || ' (owner=' || pg_get_userbyid(c.relowner) || ')'
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname ~ '^t_'
		  AND c.relkind = 'r'
		  AND pg_get_userbyid(c.relowner) !~ '^(db_tenant_admin|encore_admin)'
		ORDER BY 1
		LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blockers []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		blockers = append(blockers, line)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return blockers, nil
}

// tenantSchemaBaseProvisioned reports whether signup bootstrap created core tenant tables.
// Uses contact (not business_profile) because lazy migration must not run until tenantDDL
// has created tables that admin patches ALTER.
func tenantSchemaBaseProvisioned(ctx context.Context, schemaName string) (bool, error) {
	if !schemaNameRe.MatchString(schemaName) {
		return false, fmt.Errorf("invalid schema name: %q", schemaName)
	}
	return tenantschema.TableExists(ctx, DataDB.Stdlib(), schemaName, "contact")
}

// diffOrphanSchemas returns schemas in all but not in registered (test helper).
func diffOrphanSchemas(all, registered []string) []string {
	regSet := make(map[string]struct{}, len(registered))
	for _, s := range registered {
		regSet[s] = struct{}{}
	}
	var orphans []string
	for _, s := range all {
		if _, ok := regSet[s]; !ok {
			orphans = append(orphans, s)
		}
	}
	return orphans
}
