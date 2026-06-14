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
// Idempotent per order ID (reference_no): uses INSERT … ON CONFLICT DO NOTHING so concurrent
// calls for the same order are safe without an explicit existence check. walletID may be empty
// to use the default income wallet.
func RecordOrderCompletedIncome(ctx context.Context, tenantSchema, createdBy, orderID string, amount float64, walletID string) error {
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

	resolvedWalletID, err := resolveIncomeWallet(ctx, conn, walletID)
	if err != nil {
		return err
	}

	// Legacy soft-deleted rows bypass the partial unique index on reference_no and can
	// allow duplicate income rows when an order is completed again.
	if _, err := conn.ExecContext(ctx, `
		DELETE FROM fin_transaction
		WHERE reference_no = $1 AND type = 'income' AND deleted_at IS NOT NULL`, orderID); err != nil {
		return appErrs.Internal(err.Error())
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

	// ON CONFLICT DO NOTHING handles the rare race where two requests complete the same
	// order simultaneously. RowsAffected == 0 means a concurrent insert won → idempotent.
	res, err := conn.ExecContext(ctx, `
		INSERT INTO fin_transaction
		 (type, amount, currency, wallet_id, category_id, description, reference_no,
		  transaction_date, status, tags, created_by)
		 VALUES ('income', $1, 'IDR', $2, $3, $4, $5, $6, 'approved', '{}', $7)
		 ON CONFLICT DO NOTHING`,
		amount,
		resolvedWalletID,
		nullStr(categoryID),
		desc,
		orderID,
		today,
		createdBy,
	)
	if err != nil {
		return appErrs.Internal("failed to record order income: " + err.Error())
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // idempotent: income row already exists
	}

	refreshWallets(ctx, conn, resolvedWalletID, nil)
	return nil
}

// ResyncOrderCompletedIncome replaces the finance income row for a completed order.
// Use when total or wallet changes on an already-completed order, or when re-applying
// status=completed so ON CONFLICT DO NOTHING does not leave a stale wallet/amount.
func ResyncOrderCompletedIncome(ctx context.Context, tenantSchema, createdBy, orderID string, amount float64, walletID string) error {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil
	}
	if amount <= 0 {
		return RemoveOrderIncomeTransaction(ctx, tenantSchema, orderID)
	}
	if err := CheckCurrentPeriodUnlocked(ctx, tenantSchema); err != nil {
		return err
	}
	if err := RemoveOrderIncomeTransaction(ctx, tenantSchema, orderID); err != nil {
		return err
	}
	return RecordOrderCompletedIncome(ctx, tenantSchema, createdBy, orderID, amount, walletID)
}

// CheckCurrentPeriodUnlocked returns an error if the current finance period is locked.
// Call this before persisting an order status change to 'completed' so the order DB write
// is not committed when the subsequent finance insert would be rejected.
func CheckCurrentPeriodUnlocked(ctx context.Context, tenantSchema string) error {
	conn, err := tenant.TenantConn(ctx, tenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer conn.Close()
	today := financeToday(ctx, conn)
	return ensurePeriodUnlocked(ctx, conn, walletPeriod(today))
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

func resolveIncomeWallet(ctx context.Context, conn *sql.Conn, walletID string) (string, error) {
	walletID = strings.TrimSpace(walletID)
	if walletID != "" {
		var exists bool
		if err := conn.QueryRowContext(ctx, `
			SELECT EXISTS(
			  SELECT 1 FROM fin_wallet
			  WHERE id = $1 AND deleted_at IS NULL AND is_active = true
			)`, walletID).Scan(&exists); err != nil {
			return "", appErrs.Internal(err.Error())
		}
		if !exists {
			return "", appErrs.BadRequest("dompet pemasukan tidak valid atau tidak aktif")
		}
		return walletID, nil
	}
	return resolveDefaultIncomeWallet(ctx, conn)
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

// ValidateIncomeWallet ensures an optional wallet ID refers to an active wallet.
func ValidateIncomeWallet(ctx context.Context, tenantSchema, walletID string) error {
	if strings.TrimSpace(walletID) == "" {
		return nil
	}
	conn, err := tenant.TenantConn(ctx, tenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer conn.Close()
	_, err = resolveIncomeWallet(ctx, conn, walletID)
	return err
}
