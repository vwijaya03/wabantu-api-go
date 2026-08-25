package finance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

// RecordInventoryEntry posts an idempotent finance row for an inventory event
// (write-off / opname loss / revaluation up/down). flow must be "income" or
// "expense". referenceNo should be unique per event (e.g. the stock movement id)
// so retries are idempotent. categoryName should be a system finance category
// such as "Selisih Persediaan" or "Penyesuaian Nilai Persediaan".
// preferredWalletID may be empty to use the default income wallet (first active cash wallet).
//
// Best-effort consistency mirrors the order→income pattern: callers post this
// after the stock movement is committed and pre-check the period lock via
// CheckCurrentPeriodUnlocked so failures are caught before the stock write.
func RecordInventoryEntry(ctx context.Context, tenantSchema, createdBy, referenceNo, flow, categoryName, description string, amount float64, preferredWalletID string) error {
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
	defer tenant.CloseTenantConn(conn)

	resolvedWalletID, err := resolveIncomeWallet(ctx, conn, preferredWalletID)
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
		flow, amount, resolvedWalletID, nullStr(categoryID), description, refArg, today, createdBy); err != nil {
		return appErrs.Internal("failed to record inventory finance entry: " + err.Error())
	}
	refreshWallets(ctx, conn, resolvedWalletID, nil)
	return nil
}

// RemoveInventoryEntry deletes inventory finance rows by reference (used when an
// inventory event is reversed). Safe no-op when nothing matches.
func RemoveInventoryEntry(ctx context.Context, tenantSchema, referenceNo string) error {
	return RemoveInventoryEntries(ctx, tenantSchema, []string{referenceNo})
}

// RemoveInventoryEntries deletes finance rows for many references in one connection.
func RemoveInventoryEntries(ctx context.Context, tenantSchema string, referenceNos []string) error {
	refs := uniqueReferenceNos(referenceNos)
	if len(refs) == 0 {
		return nil
	}
	conn, err := tenant.TenantConn(ctx, tenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	const chunk = 200
	for i := 0; i < len(refs); i += chunk {
		end := i + chunk
		if end > len(refs) {
			end = len(refs)
		}
		if err := removeInventoryEntriesChunk(ctx, conn, refs[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func uniqueReferenceNos(referenceNos []string) []string {
	seen := make(map[string]struct{}, len(referenceNos))
	out := make([]string, 0, len(referenceNos))
	for _, r := range referenceNos {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

func removeInventoryEntriesChunk(ctx context.Context, conn *sql.Conn, refs []string) error {
	if len(refs) == 0 {
		return nil
	}
	clause, args := inClause(1, len(refs))
	for i, r := range refs {
		args[i] = r
	}

	rows, err := conn.QueryContext(ctx, fmt.Sprintf(
		`SELECT DISTINCT wallet_id::text FROM fin_transaction WHERE reference_no IN (%s)`, clause), args...)
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
	if err := rows.Err(); err != nil {
		return appErrs.Internal(err.Error())
	}

	if _, err := conn.ExecContext(ctx, fmt.Sprintf(
		`DELETE FROM fin_transaction WHERE reference_no IN (%s)`, clause), args...); err != nil {
		return appErrs.Internal(err.Error())
	}
	for _, w := range wallets {
		refreshWallets(ctx, conn, w, nil)
	}
	return nil
}

func inClause(startIdx int, n int) (string, []any) {
	parts := make([]string, n)
	args := make([]any, n)
	for i := 0; i < n; i++ {
		parts[i] = fmt.Sprintf("$%d", startIdx+i)
	}
	return strings.Join(parts, ","), args
}
