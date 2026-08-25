package tenant

import (
	"context"
	"database/sql"
	"fmt"

	"encore.dev"

	"encore.app/wabantu/shared/tenantschema"
)

// inventoryFinanceCategories are seeded as system finance categories so that
// COGS, purchases and stock adjustments post into recognizable buckets.
//
//	name  -> finance category type ('expense' | 'income' | 'any')
var inventoryFinanceCategories = []struct {
	name  string
	typ   string
	icon  string
	color string
	order int
}{
	{"HPP / COGS", "expense", "package", "#b91c1c", 50},
	{"Pembelian Persediaan", "expense", "package", "#9a3412", 51},
	{"Penyesuaian Nilai Persediaan", "any", "scale", "#1d4ed8", 52},
	{"Selisih Persediaan", "expense", "scale", "#7c3aed", 53},
}

// runInventorySchemaAndSeed creates inventory tables (idempotent) and seeds the
// default warehouse, singleton setting row, and inventory finance categories.
//
// Only CREATE TABLE/INDEX on new inv_* tables are issued, so this is safe to run
// at runtime on Encore Cloud (no ALTER on app-non-owned tables).
func runInventorySchemaAndSeed(ctx context.Context, conn *sql.Conn) error {
	ready, err := tenantschema.InventoryModuleReady(ctx, conn)
	if err != nil {
		return fmt.Errorf("inventory schema check: %w", err)
	}
	if !ready {
		if encore.Meta().Environment.Cloud != encore.CloudLocal {
			if err := ensureCloudAdminDDLForConn(ctx, conn); err != nil {
				return fmt.Errorf("inventory cloud DDL: %w", err)
			}
			ready, err = tenantschema.InventoryModuleReady(ctx, conn)
			if err != nil {
				return fmt.Errorf("inventory schema recheck: %w", err)
			}
			if !ready {
				return fmt.Errorf("inventory schema masih belum lengkap setelah cloud DDL")
			}
		} else if _, err := conn.ExecContext(ctx, tenantschema.InventorySchemaSQL); err != nil {
			return fmt.Errorf("inventory DDL: %w", err)
		}
	}
	if err := seedInventorySetting(ctx, conn); err != nil {
		return fmt.Errorf("inventory seed setting: %w", err)
	}
	if err := seedDefaultWarehouse(ctx, conn); err != nil {
		return fmt.Errorf("inventory seed warehouse: %w", err)
	}
	if err := seedInventoryFinanceCategories(ctx, conn); err != nil {
		return fmt.Errorf("inventory seed finance categories: %w", err)
	}
	return nil
}

// RunInventorySchemaPatches applies inventory DDL + seed on a single tenant schema.
// Exposed for migration scripts / targeted backfills (mirrors RunEventsSchemaPatches).
func RunInventorySchemaPatches(ctx context.Context, schemaName string) error {
	if !schemaNameRe.MatchString(schemaName) {
		return fmt.Errorf("invalid schema name: %q", schemaName)
	}
	conn, err := TenantConn(ctx, schemaName)
	if err != nil {
		return err
	}
	defer conn.Close()
	return runInventorySchemaAndSeed(ctx, conn)
}

func seedInventorySetting(ctx context.Context, conn *sql.Conn) error {
	var n int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM inv_setting`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := conn.ExecContext(ctx, `
		INSERT INTO inv_setting (setup_completed, default_costing_method, block_negative_stock)
		VALUES (false, 'average', true)`)
	return err
}

func seedDefaultWarehouse(ctx context.Context, conn *sql.Conn) error {
	var n int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM inv_warehouse WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := conn.ExecContext(ctx, `
		INSERT INTO inv_warehouse (code, name, external_location_id, is_default, is_active, display_order)
		VALUES ('gudang-utama', 'Gudang Utama', -1, true, true, 0)`)
	return err
}

func seedInventoryFinanceCategories(ctx context.Context, conn *sql.Conn) error {
	// Finance tables may legitimately be absent on a very old schema mid-migration;
	// skip silently if so (the finance seed step earlier in the chain creates them).
	var hasFinCategory bool
	if err := conn.QueryRowContext(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM information_schema.tables
		  WHERE table_schema = current_schema() AND table_name = 'fin_category'
		)`).Scan(&hasFinCategory); err != nil {
		return err
	}
	if !hasFinCategory {
		return nil
	}
	for _, c := range inventoryFinanceCategories {
		var exists bool
		if err := conn.QueryRowContext(ctx, `
			SELECT EXISTS(
			  SELECT 1 FROM fin_category
			  WHERE name = $1 AND is_system = true AND parent_id IS NULL AND deleted_at IS NULL
			)`, c.name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO fin_category (name, type, icon, color, is_system, display_order)
			VALUES ($1, $2, $3, $4, true, $5)`,
			c.name, c.typ, c.icon, c.color, c.order); err != nil {
			return err
		}
	}
	return nil
}
