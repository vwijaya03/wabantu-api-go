package finance

import (
	"context"
	"database/sql"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

// RecordInventoryEntry posts an idempotent finance row for an inventory event
// (write-off / opname loss / revaluation up/down). flow must be "income" or
// "expense". referenceNo should be unique per event (e.g. the stock movement id)
// so retries are idempotent. categoryName should be a system finance category
// such as "Selisih Persediaan" or "Penyesuaian Nilai Persediaan".
//
// Best-effort consistency mirrors the order→income pattern: callers post this
// after the stock movement is committed and pre-check the period lock via
// CheckCurrentPeriodUnlocked so failures are caught before the stock write.
func RecordInventoryEntry(ctx context.Context, tenantSchema, createdBy, referenceNo, flow, categoryName, description string, amount float64) error {
	if amount <= 0 {
		return nil
	}
	flow = strings.TrimSpace(flow)
	if flow != "income" && flow != "expense" {
		return appErrs.Internal("inventory finance flow tidak valid")
	}
	referenceNo = strings.TrimSpace(referenceNo)

	conn, err := tenant.TenantConn(ctx, tenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer conn.Close()

	walletID, err := resolveDefaultIncomeWallet(ctx, conn)
	if err != nil {
		return err
	}

	var categoryID sql.NullString
	_ = conn.QueryRowContext(ctx, `
		SELECT id::text FROM fin_category
		WHERE name = $1 AND deleted_at IS NULL AND is_system = true
		LIMIT 1`, categoryName).Scan(&categoryID)

	today := financeToday(ctx, conn)
	if err := ensurePeriodUnlocked(ctx, conn, walletPeriod(today)); err != nil {
		return err
	}

	if createdBy == "" {
		createdBy = "00000000-0000-0000-0000-000000000000"
	}

	if referenceNo != "" {
		var exists bool
		if err := conn.QueryRowContext(ctx, `
			SELECT EXISTS(
			  SELECT 1 FROM fin_transaction
			  WHERE reference_no = $1 AND type = $2 AND deleted_at IS NULL
			)`, referenceNo, flow).Scan(&exists); err != nil {
			return appErrs.Internal(err.Error())
		}
		if exists {
			return nil
		}
	}

	var refArg any
	if referenceNo != "" {
		refArg = referenceNo
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO fin_transaction
		 (type, amount, currency, wallet_id, category_id, description, reference_no,
		  transaction_date, status, tags, created_by)
		 VALUES ($1, $2, 'IDR', $3, $4, $5, $6, $7, 'approved', '{}', $8)`,
		flow, amount, walletID, nullStr(categoryID), description, refArg, today, createdBy); err != nil {
		return appErrs.Internal("failed to record inventory finance entry: " + err.Error())
	}
	refreshWallets(ctx, conn, walletID, nil)
	return nil
}

// RemoveInventoryEntry deletes inventory finance rows by reference (used when an
// inventory event is reversed). Safe no-op when nothing matches.
func RemoveInventoryEntry(ctx context.Context, tenantSchema, referenceNo string) error {
	referenceNo = strings.TrimSpace(referenceNo)
	if referenceNo == "" {
		return nil
	}
	conn, err := tenant.TenantConn(ctx, tenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx,
		`SELECT DISTINCT wallet_id::text FROM fin_transaction WHERE reference_no = $1`, referenceNo)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer rows.Close()
	var wallets []string
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			return appErrs.Internal(err.Error())
		}
		wallets = append(wallets, w)
	}

	if _, err := conn.ExecContext(ctx,
		`DELETE FROM fin_transaction WHERE reference_no = $1`, referenceNo); err != nil {
		return appErrs.Internal(err.Error())
	}
	for _, w := range wallets {
		refreshWallets(ctx, conn, w, nil)
	}
	return nil
}
