package inventory

import (
	"context"
	"testing"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/tenant"
)

// TestResyncOrderCOGSUsesOrderIncomeWallet asserts HPP expense is posted to the same
// wallet as order.income_wallet_id (not the default cash wallet).
func TestResyncOrderCOGSUsesOrderIncomeWallet(t *testing.T) {
	if testing.Short() {
		t.Skip("requires tenant database")
	}

	ctx := context.Background()
	schema := "t_test_cogs_wallet"
	if err := tenant.RunTenantDDL(ctx, schema); err != nil {
		t.Fatalf("RunTenantDDL: %v", err)
	}

	pool := tenantDB()
	sch := appdb.SchemaSQL{Schema: schema}

	const (
		userID          = "00000000-0000-0000-0000-000000000001"
		defaultWalletID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
		incomeWalletID  = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
		orderID         = "cccccccc-cccc-cccc-cccc-cccccccccccc"
		catalogItemID   = "dddddddd-dddd-dddd-dddd-dddddddddddd"
		warehouseID     = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	)

	// Secondary wallet (order income target) — default seed wallet remains first cash wallet.
	if _, err := qexec(ctx, sch, pool, `
		INSERT INTO fin_wallet (id, name, type, color, icon, is_active, visibility, display_order, created_by)
		VALUES ($1::uuid, 'Rekening OCBC', 'bank', '#1D4ED8', 'landmark', true, 'all', 1, $2::uuid)`,
		incomeWalletID, userID); err != nil {
		t.Fatalf("insert income wallet: %v", err)
	}
	if _, err := qexec(ctx, sch, pool, `
		INSERT INTO fin_wallet_balance (wallet_id, balance) VALUES ($1::uuid, 0)`,
		incomeWalletID); err != nil {
		t.Fatalf("insert income wallet balance: %v", err)
	}

	if _, err := qexec(ctx, sch, pool, `
		INSERT INTO "order" (id, income_wallet_id, status, items, created_by)
		VALUES ($1::uuid, $2::uuid, 'completed', '[]', $3::uuid)`,
		orderID, incomeWalletID, userID); err != nil {
		t.Fatalf("insert order: %v", err)
	}

	if _, err := qexec(ctx, sch, pool, `
		INSERT INTO inv_stock_movement (
			catalog_item_id, warehouse_id, movement_type, direction,
			qty, unit_cost, total_cost, ref_type, ref_id, created_by
		) VALUES ($1::uuid, $2::uuid, 'sale_issue', 'out', 1, 5000, 5000, 'order', $3::uuid, $4::uuid)`,
		catalogItemID, warehouseID, orderID, userID); err != nil {
		t.Fatalf("insert stock movement: %v", err)
	}

	if err := resyncOrderCOGS(ctx, schema, sch, pool, orderID, false, userID); err != nil {
		t.Fatalf("resyncOrderCOGS: %v", err)
	}

	var gotWallet string
	if err := qrow(ctx, sch, pool, `
		SELECT wallet_id::text FROM fin_transaction
		WHERE reference_no = $1 AND type = 'expense' AND deleted_at IS NULL`,
		cogsRefPrefix+orderID).Scan(&gotWallet); err != nil {
		t.Fatalf("query HPP transaction: %v", err)
	}
	if gotWallet != incomeWalletID {
		t.Fatalf("HPP wallet_id = %q, want order income_wallet_id %q (default would be %q)",
			gotWallet, incomeWalletID, defaultWalletID)
	}
}
