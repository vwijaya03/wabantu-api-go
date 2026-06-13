package finance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

// RecordOrderCompletedIncome creates an approved income transaction when an order is completed.
// Idempotent per order ID (reference_no).
func RecordOrderCompletedIncome(ctx context.Context, tenantSchema, createdBy, orderID string, amount float64) error {
	if amount <= 0 {
		return nil
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil
	}

	conn, err := tenant.TenantConn(ctx, tenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var exists bool
	if err := conn.QueryRowContext(ctx, `
		SELECT EXISTS(
		  SELECT 1 FROM fin_transaction
		  WHERE reference_no = $1 AND type = 'income' AND deleted_at IS NULL
		  LIMIT 1
		)`, orderID).Scan(&exists); err != nil {
		return appErrs.Internal(err.Error())
	}
	if exists {
		return nil
	}

	// Legacy soft-deleted rows would bypass the active-only check and cause duplicates on re-complete.
	if _, err := conn.ExecContext(ctx, `
		DELETE FROM fin_transaction
		WHERE reference_no = $1 AND type = 'income' AND deleted_at IS NOT NULL`, orderID); err != nil {
		return appErrs.Internal(err.Error())
	}

	walletID, err := resolveDefaultIncomeWallet(ctx, conn)
	if err != nil {
		return err
	}

	var categoryID sql.NullString
	_ = conn.QueryRowContext(ctx, `
		SELECT id::text FROM fin_category
		WHERE name = 'Penjualan Produk' AND deleted_at IS NULL AND is_system = true
		LIMIT 1`).Scan(&categoryID)

	today := financeToday(ctx, conn)
	if err := ensurePeriodUnlocked(ctx, conn, walletPeriod(today)); err != nil {
		return err
	}

	desc := fmt.Sprintf("Pesanan #%s", orderID)
	if createdBy == "" {
		createdBy = "00000000-0000-0000-0000-000000000000"
	}

	var txnID string
	err = conn.QueryRowContext(ctx, `
		INSERT INTO fin_transaction
		 (type, amount, currency, wallet_id, category_id, description, reference_no,
		  transaction_date, status, tags, created_by)
		 VALUES ('income', $1, 'IDR', $2, $3, $4, $5, $6, 'approved', '{}', $7)
		 RETURNING id`,
		amount,
		walletID,
		nullStr(categoryID),
		desc,
		orderID,
		today,
		createdBy,
	).Scan(&txnID)
	if err != nil {
		return appErrs.Internal("failed to record order income: " + err.Error())
	}

	refreshWallets(ctx, conn, walletID, nil)
	return nil
}

// RemoveOrderIncomeTransaction deletes income rows linked to an order (reference_no).
func RemoveOrderIncomeTransaction(ctx context.Context, tenantSchema, orderID string) error {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil
	}

	conn, err := tenant.TenantConn(ctx, tenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx, `
		SELECT DISTINCT wallet_id::text
		FROM fin_transaction
		WHERE reference_no = $1 AND type = 'income'`, orderID)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer rows.Close()

	walletIDs := make([]string, 0)
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			return appErrs.Internal(err.Error())
		}
		walletIDs = append(walletIDs, w)
	}
	if err := rows.Err(); err != nil {
		return appErrs.Internal(err.Error())
	}

	res, err := conn.ExecContext(ctx, `
		DELETE FROM fin_transaction
		WHERE reference_no = $1 AND type = 'income'`, orderID)
	if err != nil {
		return appErrs.Internal("failed to remove order income transaction")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}

	for _, w := range walletIDs {
		refreshWallets(ctx, conn, w, nil)
	}
	return nil
}

func resolveDefaultIncomeWallet(ctx context.Context, conn *sql.Conn) (string, error) {
	var walletID string
	err := conn.QueryRowContext(ctx, `
		SELECT id::text FROM fin_wallet
		WHERE deleted_at IS NULL AND is_active = true
		ORDER BY CASE WHEN type = 'cash' THEN 0 ELSE 1 END, display_order, created_at
		LIMIT 1`).Scan(&walletID)
	if err == sql.ErrNoRows {
		return "", appErrs.BadRequest("belum ada dompet aktif untuk mencatat pemasukan pesanan")
	}
	if err != nil {
		return "", appErrs.Internal(err.Error())
	}
	return walletID, nil
}
