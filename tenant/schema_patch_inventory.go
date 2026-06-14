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
	hasCol, err := tenantschema.ColumnExists(ctx, conn, "inv_setting", "purchase_posts_expense")
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
	_, err = conn.ExecContext(ctx, inventoryPurchasePostsExpenseAlterSQL)
	return err
}
