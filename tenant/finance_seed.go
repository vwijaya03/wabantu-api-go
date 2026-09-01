package tenant

import (
	"context"
	"database/sql"
)

// seedFinanceTransactionTypes inserts default transaction type labels (idempotent).
func seedFinanceTransactionTypes(ctx context.Context, conn *sql.Conn) error {
	type row struct {
		code, label, flow, categoryKind string
		showInQuick                     bool
		order                           int
		ownerOnly, isSystem             bool
	}
	rows := []row{
		{"income", "Pemasukan", "income", "income", true, 1, false, true},
		{"expense", "Pengeluaran", "expense", "expense", true, 2, false, true},
		{"transfer", "Transfer", "transfer", "any", true, 3, false, true},
		{"investment_buy", "Beli Aset", "expense", "investment", false, 4, false, true},
		{"investment_sell", "Jual Aset", "income", "investment", false, 5, false, true},
		{"dividend", "Dividen", "income", "income", false, 6, false, true},
		{"interest", "Bunga", "income", "income", false, 7, false, true},
		{"cashback", "Cashback", "income", "income", false, 8, false, true},
		{"adjustment", "Penyesuaian (Owner)", "adjustment", "any", false, 9, true, true},
	}
	for _, r := range rows {
		var exists bool
		if err := conn.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM fin_transaction_type WHERE code=$1 AND deleted_at IS NULL)`, r.code,
		).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		_, err := conn.ExecContext(ctx, `
			INSERT INTO fin_transaction_type
			 (code, label, flow, category_kind, show_in_quick, display_order, is_system, owner_only, is_active)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,true)`,
			r.code, r.label, r.flow, r.categoryKind, r.showInQuick, r.order, r.isSystem, r.ownerOnly,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// seedFinanceCategories inserts default categories idempotently.
func seedFinanceCategories(ctx context.Context, conn *sql.Conn) error {
	type cat struct {
		name     string
		typ      string
		parentID *string
		icon     string
		color    string
		order    int
	}

	ensureParent := func(p cat) (string, error) {
		var id string
		err := conn.QueryRowContext(ctx, `
			SELECT id FROM fin_category
			WHERE name=$1 AND is_system=true AND parent_id IS NULL AND deleted_at IS NULL
			ORDER BY display_order, created_at, id
			LIMIT 1`, p.name,
		).Scan(&id)
		if err == nil {
			return id, nil
		}
		if err != sql.ErrNoRows {
			return "", err
		}
		err = conn.QueryRowContext(ctx, `
			INSERT INTO fin_category (name, type, icon, color, is_system, display_order)
			VALUES ($1,$2,$3,$4,true,$5)
			RETURNING id`,
			p.name, p.typ, p.icon, p.color, p.order,
		).Scan(&id)
		if err == nil {
			return id, nil
		}
		if isPgUniqueViolation(err) {
			err = conn.QueryRowContext(ctx, `
				SELECT id FROM fin_category
				WHERE name=$1 AND is_system=true AND parent_id IS NULL AND deleted_at IS NULL
				ORDER BY display_order, created_at, id
				LIMIT 1`, p.name,
			).Scan(&id)
		}
		return id, err
	}

	ensureChild := func(c cat) error {
		if c.parentID == nil || *c.parentID == "" {
			return nil
		}
		var exists bool
		if err := conn.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM fin_category
				WHERE name=$1 AND parent_id=$2 AND is_system=true AND deleted_at IS NULL
			)`, c.name, *c.parentID,
		).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
		_, err := conn.ExecContext(ctx, `
			INSERT INTO fin_category (name, type, parent_id, icon, color, is_system, display_order)
			VALUES ($1,$2,$3,$4,$5,true,$6)`,
			c.name, c.typ, *c.parentID, c.icon, c.color, c.order,
		)
		if err == nil || isPgUniqueViolation(err) {
			return nil
		}
		return err
	}

	parents := []cat{
		{"Pemasukan", "income", nil, "trending-up", "#16A34A", 1},
		{"Pengeluaran", "expense", nil, "trending-down", "#DC2626", 2},
		{"Investasi", "investment", nil, "bar-chart-2", "#7C3AED", 3},
	}

	parentIDs := map[string]string{}
	for _, p := range parents {
		id, err := ensureParent(p)
		if err != nil {
			return err
		}
		parentIDs[p.name] = id
	}

	incID := parentIDs["Pemasukan"]
	expID := parentIDs["Pengeluaran"]
	invID := parentIDs["Investasi"]

	children := []cat{
		{"Penjualan Produk", "income", &incID, "shopping-bag", "#16A34A", 1},
		{"Jasa / Layanan", "income", &incID, "briefcase", "#16A34A", 2},
		{"Komisi", "income", &incID, "percent", "#16A34A", 3},
		{"Lain-lain (Pemasukan)", "income", &incID, "plus-circle", "#16A34A", 4},
		{"Gaji & Upah", "expense", &expID, "users", "#DC2626", 1},
		{"Sewa & Utilitas", "expense", &expID, "home", "#DC2626", 2},
		{"Transport", "expense", &expID, "truck", "#DC2626", 3},
		{"Pemasaran", "expense", &expID, "megaphone", "#DC2626", 4},
		{"Makan & Minum", "expense", &expID, "coffee", "#DC2626", 5},
		{"Perlengkapan Kantor", "expense", &expID, "package", "#DC2626", 6},
		{"Beli Stok / Bahan Baku", "expense", &expID, "archive", "#DC2626", 7},
		{"Lain-lain (Pengeluaran)", "expense", &expID, "minus-circle", "#DC2626", 8},
		{"Saham", "investment", &invID, "trending-up", "#7C3AED", 1},
		{"Kripto", "investment", &invID, "zap", "#7C3AED", 2},
		{"Emas", "investment", &invID, "star", "#7C3AED", 3},
		{"Reksa Dana", "investment", &invID, "pie-chart", "#7C3AED", 4},
		{"Properti", "investment", &invID, "building", "#7C3AED", 5},
		{"Lain-lain (Investasi)", "investment", &invID, "circle", "#7C3AED", 6},
	}

	for _, c := range children {
		if err := ensureChild(c); err != nil {
			return err
		}
	}
	return nil
}

// seedFinanceWallet creates a default Cash wallet.
func seedFinanceWallet(ctx context.Context, conn *sql.Conn) error {
	var exists bool
	conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fin_wallet LIMIT 1)`).Scan(&exists)
	if exists {
		return nil
	}
	_, err := conn.ExecContext(ctx,
		`INSERT INTO fin_wallet (name, type, color, icon, is_active, visibility, display_order, created_by)
		 VALUES ('Kas Tunai','cash','#16A34A','banknote',true,'all',0,'00000000-0000-0000-0000-000000000000')
		 ON CONFLICT DO NOTHING`,
	)
	if err != nil {
		return err
	}
	conn.ExecContext(ctx,
		`INSERT INTO fin_wallet_balance (wallet_id, balance)
		 SELECT id, initial_balance FROM fin_wallet WHERE type='cash' LIMIT 1
		 ON CONFLICT DO NOTHING`,
	)
	return nil
}

// seedFinanceApprovalSetting creates the default (disabled) approval setting row.
func seedFinanceApprovalSetting(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx,
		`INSERT INTO fin_approval_setting (id, enabled, require_for_types)
		 VALUES ('00000000-0000-0000-0000-000000000001', false, '{}')
		 ON CONFLICT (id) DO NOTHING`,
	)
	return err
}
