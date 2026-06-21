package tenant

import (
	"context"
	"database/sql"
	"fmt"

	"encore.dev"

	"encore.app/wabantu/shared/tenantschema"
)

const inventoryPurchasePostsExpenseAlterSQL = `
ALTER TABLE inv_setting ADD COLUMN IF NOT EXISTS purchase_posts_expense BOOLEAN NOT NULL DEFAULT false;
`

const inventoryStockTxnBackfillAlterSQL = `
ALTER TABLE inv_setting ADD COLUMN IF NOT EXISTS stock_txn_backfill_done BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS idx_inv_movement_orphan_backfill
    ON inv_stock_movement(created_at)
    WHERE ref_id IS NULL
      AND movement_type IN ('adjustment_plus','adjustment_minus','opening_balance','transfer_out','revaluation_cost');
CREATE INDEX IF NOT EXISTS idx_inv_stock_txn_line_item_wh
    ON inv_stock_transaction_line(catalog_item_id, warehouse_id);
`

const inventoryWarehouseCustomerLabelAlterSQL = `
ALTER TABLE inv_warehouse ADD COLUMN IF NOT EXISTS customer_label VARCHAR(80);
`

// alwaysApplyInventorySettingPatch ensures inv_setting has PR-A6 columns on local dev.
// On Encore Cloud, admin-owned tables must be patched via apply-inventory-schema-cloud.sh.
func alwaysApplyInventorySettingPatch(ctx context.Context, conn *sql.Conn) error {
	hasTable, err := tenantschema.TableExists(ctx, conn, "inv_setting")
	if err != nil {
		return err
	}
	if !hasTable {
		return nil
	}
	if err := applyInventoryColumnPatch(ctx, conn, "purchase_posts_expense", inventoryPurchasePostsExpenseAlterSQL); err != nil {
		return err
	}
	if err := applyInventoryColumnPatch(ctx, conn, "stock_txn_backfill_done", inventoryStockTxnBackfillAlterSQL); err != nil {
		return err
	}
	return applyWarehouseCustomerLabelPatch(ctx, conn)
}

const inventoryStockTxnLineItemWhIndexSQL = `
CREATE INDEX IF NOT EXISTS idx_inv_stock_txn_line_item_wh
    ON inv_stock_transaction_line(catalog_item_id, warehouse_id);
`

// alwaysApplyInventoryIndexPatch adds indexes safe to re-run on every migration.
func alwaysApplyInventoryIndexPatch(ctx context.Context, conn *sql.Conn) error {
	hasTable, err := tenantschema.TableExists(ctx, conn, "inv_stock_transaction_line")
	if err != nil {
		return err
	}
	if !hasTable {
		return nil
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return nil
	}
	_, err = conn.ExecContext(ctx, inventoryStockTxnLineItemWhIndexSQL)
	return err
}

func applyInventoryColumnPatch(ctx context.Context, conn *sql.Conn, column, alterSQL string) error {
	hasCol, err := tenantschema.ColumnExists(ctx, conn, "inv_setting", column)
	if err != nil {
		return err
	}
	if hasCol {
		return nil
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return fmt.Errorf(
			"patch inventory belum diterapkan di cloud: jalankan ./scripts/apply-inventory-schema-cloud.sh %s",
			encore.Meta().Environment.Name,
		)
	}
	_, err = conn.ExecContext(ctx, alterSQL)
	return err
}

func applyWarehouseCustomerLabelPatch(ctx context.Context, conn *sql.Conn) error {
	hasTable, err := tenantschema.TableExists(ctx, conn, "inv_warehouse")
	if err != nil {
		return err
	}
	if !hasTable {
		return nil
	}
	hasCol, err := tenantschema.ColumnExists(ctx, conn, "inv_warehouse", "customer_label")
	if err != nil {
		return err
	}
	if hasCol {
		return nil
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return fmt.Errorf(
			"patch inventory belum diterapkan di cloud: jalankan ./scripts/apply-inventory-schema-cloud.sh %s",
			encore.Meta().Environment.Name,
		)
	}
	_, err = conn.ExecContext(ctx, inventoryWarehouseCustomerLabelAlterSQL)
	return err
}
