package finance

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"encore.dev/rlog"

	appdb "encore.app/wabantu/shared/db"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/types"
)

func encryptFinanceTitle(title string) (enc string, storeTitle string, err error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", "", nil
	}
	enc, err = pii.Encrypt(title, strings.TrimSpace(secrets.DataEncryptionKey))
	if err != nil {
		return "", "", err
	}
	return enc, pii.Placeholder, nil
}

func decryptFinanceTitle(enc, legacy string) (string, error) {
	return pii.DecryptOrLegacy(enc, legacy, strings.TrimSpace(secrets.DataEncryptionKey))
}

// Singleton row for tenant approval settings.
const approvalSettingID = "00000000-0000-0000-0000-000000000001"

// parsePostgreSQLStringArray decodes TEXT[] returned as string by some drivers (e.g. "{}").
func parsePostgreSQLStringArray(raw sql.NullString) []string {
	if !raw.Valid {
		return []string{}
	}
	s := strings.TrimSpace(raw.String)
	if s == "" || s == "{}" {
		return []string{}
	}
	if strings.HasPrefix(s, "[") {
		var tags []string
		if err := json.Unmarshal([]byte(s), &tags); err == nil && tags != nil {
			return tags
		}
	}
	inner := strings.TrimPrefix(strings.TrimSuffix(s, "}"), "{")
	if inner == "" {
		return []string{}
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.Trim(p, `"`))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func refreshWallets(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, walletID string, toWalletID *string) {
	if walletID != "" {
		if err := refreshWalletBalance(ctx, sch, q, walletID); err != nil {
			rlog.Warn("finance: refresh wallet balance", "walletId", walletID, "err", err)
		}
	}
	if toWalletID != nil && *toWalletID != "" && *toWalletID != walletID {
		if err := refreshWalletBalance(ctx, sch, q, *toWalletID); err != nil {
			rlog.Warn("finance: refresh wallet balance", "walletId", *toWalletID, "err", err)
		}
	}
}

func refreshWalletsForTransaction(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, txnID string) {
	var walletID string
	var toWalletID sql.NullString
	if err := qrow(ctx, sch, q,
		`SELECT wallet_id, to_wallet_id FROM fin_transaction WHERE id=$1`, txnID,
	).Scan(&walletID, &toWalletID); err != nil {
		rlog.Warn("finance: load txn for balance refresh", "txnId", txnID, "err", err)
		return
	}
	var toPtr *string
	if toWalletID.Valid && toWalletID.String != "" {
		toPtr = &toWalletID.String
	}
	refreshWallets(ctx, sch, q, walletID, toPtr)
}

func assertWalletAccessible(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, u *types.AuthUser, walletID string) error {
	if walletID == "" {
		return appErrs.BadRequest("dompet harus dipilih")
	}
	if isOwner(u) {
		return nil
	}
	var visibility string
	err := qrow(ctx, sch, q,
		`SELECT visibility FROM fin_wallet WHERE id=$1 AND deleted_at IS NULL AND is_active=true`, walletID,
	).Scan(&visibility)
	if err == sql.ErrNoRows {
		return appErrs.BadRequest("dompet tidak ditemukan")
	}
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	if visibility == "owner" {
		return appErrs.Forbidden("dompet ini hanya dapat diakses owner")
	}
	return nil
}

// staffTxnVisibilitySQL restricts staff to non–owner-only wallets (alias w = source wallet).
func staffTxnVisibilitySQL() string {
	return ` AND w.deleted_at IS NULL AND w.visibility='all'
		AND (t.to_wallet_id IS NULL OR tw.deleted_at IS NULL AND tw.visibility='all')`
}

func flowFallbackSQL(col string) string {
	return `COALESCE(tt.flow,
		CASE WHEN ` + col + ` IN ('income','dividend','interest','cashback','investment_sell') THEN 'income'
		     WHEN ` + col + ` IN ('expense','investment_buy') THEN 'expense'
		     WHEN ` + col + ` = 'transfer' THEN 'transfer'
		     WHEN ` + col + ` = 'adjustment' THEN 'adjustment'
		     ELSE 'expense' END)`
}

func staffWalletBalanceFilter() string {
	return ` AND w.visibility='all'`
}

type approvalConfig struct {
	enabled         bool
	threshold       sql.NullFloat64
	requireForTypes []string
}

func loadApprovalConfig(ctx context.Context, sch appdb.SchemaSQL, q finQuerier) (approvalConfig, error) {
	var cfg approvalConfig
	err := qrow(ctx, sch, q,
		`SELECT enabled, amount_threshold, require_for_types
		 FROM fin_approval_setting WHERE id=$1`, approvalSettingID,
	).Scan(&cfg.enabled, &cfg.threshold, &cfg.requireForTypes)
	if err == sql.ErrNoRows {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if cfg.requireForTypes == nil {
		cfg.requireForTypes = []string{}
	}
	return cfg, nil
}

func staffNeedsApproval(cfg approvalConfig, txnType string, amount float64) bool {
	if !cfg.enabled {
		return false
	}
	if len(cfg.requireForTypes) > 0 {
		found := false
		for _, t := range cfg.requireForTypes {
			if t == txnType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if cfg.threshold.Valid && amount < cfg.threshold.Float64 {
		return false
	}
	return true
}

func ensurePeriodUnlocked(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, period string) error {
	locked, err := periodLocked(ctx, sch, q, period)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	if locked {
		return appErrs.Forbidden("periode " + period + " sudah dikunci")
	}
	return nil
}

func financeTablesReady(ctx context.Context, sch appdb.SchemaSQL, q finQuerier) bool {
	var name sql.NullString
	_ = qrow(ctx, sch, q, `SELECT to_regclass($1)::text`, sch.T("fin_recurring")).Scan(&name)
	return name.Valid && strings.TrimSpace(name.String) != ""
}
