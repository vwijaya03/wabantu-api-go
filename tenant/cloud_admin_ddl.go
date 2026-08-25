package tenant

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"encore.app/wabantu/shared/tenantschema"
)

// cloudAdminDDLBlock is one idempotent SQL bundle applied with db_tenant_admin on Encore Cloud.
type cloudAdminDDLBlock struct {
	label  string
	sql    string
	covers string // documents which CloudTenantReady checks this satisfies
}

// cloudAdminTenantDDLBlocks is the single registry of admin-owned tenant DDL on Encore Cloud.
// Keep in sync with tenantschema.CloudTenantReady — add a block here when a new ready check
// requires CREATE/ALTER that the app role cannot run.
func cloudAdminTenantDDLBlocks() []cloudAdminDDLBlock {
	return []cloudAdminDDLBlock{
		{
			label:  "cloud tenant patch",
			sql:    tenantschema.CloudTenantPatchSQL,
			covers: "TenantPatchReady, PricingReady, OrderIncomePatchReady, OrderPaymentProofPatchReady, KnowledgeBaseReady",
		},
		{
			label:  "pii patch",
			sql:    tenantschema.PIISchemaPatchSQL,
			covers: "PIIReady",
		},
		{
			label:  "finance patch",
			sql:    financeSchemaPatchSQL,
			covers: "FinanceModuleReady",
		},
		{
			label:  "events patch",
			sql:    eventsSchemaPatchSQL,
			covers: "EventsModuleReady",
		},
		{
			label:  "inventory patch",
			sql:    tenantschema.InventorySchemaSQL,
			covers: "InventoryModuleReady",
		},
	}
}

// EnsureCloudAdminTenantDDL applies all admin-owned tenant DDL on Encore Cloud (idempotent).
// Safe at signup (RunTenantDDL), migrate (ProcessTenantSchemaMigration), and runtime recovery.
func EnsureCloudAdminTenantDDL(ctx context.Context, schemaName string) error {
	return applyCloudAdminTenantDDL(ctx, schemaName)
}

// prepareCloudSchemaForAdminDDL transfers table ownership to db_tenant_admin before
// applyCloudAdminTenantDDL. Uses the tenantDDL connection first (reliable encore_container
// owner transfer), then repair_tenant_schema_grants for anything left.
func prepareCloudSchemaForAdminDDL(ctx context.Context, conn *sql.Conn, schemaName string) error {
	if !isEncoreCloud() {
		return nil
	}
	if err := ensureCloudSchemaDeployGrants(ctx, conn, schemaName); err != nil {
		return err
	}
	return RepairTenantSchemaDeployGrants(ctx, schemaName)
}

// applyCloudAdminTenantDDL runs idempotent admin-owned DDL patches on Encore Cloud.
func applyCloudAdminTenantDDL(ctx context.Context, schemaName string) error {
	if !isEncoreCloud() {
		return nil
	}
	return withTenantAdminTx(ctx, schemaName, func(ctx context.Context, tx *sql.Tx) error {
		for _, block := range cloudAdminTenantDDLBlocks() {
			if strings.TrimSpace(block.sql) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, block.sql); err != nil {
				return fmt.Errorf("%s: %w", block.label, err)
			}
		}
		return nil
	})
}

// applyCloudInventoryDDL creates inv_* tables via db_tenant_admin on Encore Cloud.
func applyCloudInventoryDDL(ctx context.Context, schemaName string) error {
	if !isEncoreCloud() {
		return nil
	}
	return withTenantAdminTx(ctx, schemaName, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, tenantschema.InventorySchemaSQL)
		return err
	})
}

func ensureCloudAdminDDLForConn(ctx context.Context, conn *sql.Conn) error {
	if !isEncoreCloud() {
		return nil
	}
	schemaName, err := currentSchemaName(ctx, conn)
	if err != nil {
		return err
	}
	return applyCloudAdminTenantDDL(ctx, schemaName)
}

func currentSchemaName(ctx context.Context, conn *sql.Conn) (string, error) {
	var schemaName string
	if err := conn.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schemaName); err != nil {
		return "", fmt.Errorf("current_schema: %w", err)
	}
	if schemaName == "" {
		return "", fmt.Errorf("current_schema: empty")
	}
	return schemaName, nil
}

func withTenantAdminTx(ctx context.Context, schemaName string, fn func(context.Context, *sql.Tx) error) error {
	tx, err := DataDB.Stdlib().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL ROLE %s", cloudDBTenantAdmin)); err != nil {
		var currentUser string
		_ = tx.QueryRowContext(ctx, `SELECT current_user`).Scan(&currentUser)
		return fmt.Errorf(
			"set tenant admin role as %s — jalankan POST /api/v1/admin/migrate-tenant-schemas: %w",
			currentUser, err,
		)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`SET LOCAL search_path TO %s, public`, quoteIdent(schemaName))); err != nil {
		return fmt.Errorf("set search_path: %w", err)
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
