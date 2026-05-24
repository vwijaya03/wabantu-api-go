package tenant

import (
	"context"
	"database/sql"
)

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

	// parent groups
	parents := []cat{
		{"Pemasukan", "income", nil, "trending-up", "#16A34A", 1},
		{"Pengeluaran", "expense", nil, "trending-down", "#DC2626", 2},
		{"Investasi", "investment", nil, "bar-chart-2", "#7C3AED", 3},
	}

	parentIDs := map[string]string{}
	for _, p := range parents {
		var id string
		err := conn.QueryRowContext(ctx,
			`INSERT INTO fin_category (name, type, icon, color, is_system, display_order)
			 VALUES ($1,$2,$3,$4,true,$5)
			 ON CONFLICT DO NOTHING
			 RETURNING id`,
			p.name, p.typ, p.icon, p.color, p.order,
		).Scan(&id)
		if err != nil {
			// already exists — fetch id
			err2 := conn.QueryRowContext(ctx,
				`SELECT id FROM fin_category WHERE name=$1 AND is_system=true LIMIT 1`, p.name,
			).Scan(&id)
			if err2 != nil {
				continue
			}
		}
		parentIDs[p.name] = id
	}

	incID := parentIDs["Pemasukan"]
	expID := parentIDs["Pengeluaran"]
	invID := parentIDs["Investasi"]

	children := []cat{
		// Pemasukan
		{"Penjualan Produk", "income", &incID, "shopping-bag", "#16A34A", 1},
		{"Jasa / Layanan", "income", &incID, "briefcase", "#16A34A", 2},
		{"Komisi", "income", &incID, "percent", "#16A34A", 3},
		{"Lain-lain (Pemasukan)", "income", &incID, "plus-circle", "#16A34A", 4},
		// Pengeluaran
		{"Gaji & Upah", "expense", &expID, "users", "#DC2626", 1},
		{"Sewa & Utilitas", "expense", &expID, "home", "#DC2626", 2},
		{"Transport", "expense", &expID, "truck", "#DC2626", 3},
		{"Pemasaran", "expense", &expID, "megaphone", "#DC2626", 4},
		{"Makan & Minum", "expense", &expID, "coffee", "#DC2626", 5},
		{"Perlengkapan Kantor", "expense", &expID, "package", "#DC2626", 6},
		{"Beli Stok / Bahan Baku", "expense", &expID, "archive", "#DC2626", 7},
		{"Lain-lain (Pengeluaran)", "expense", &expID, "minus-circle", "#DC2626", 8},
		// Investasi
		{"Saham", "investment", &invID, "trending-up", "#7C3AED", 1},
		{"Kripto", "investment", &invID, "zap", "#7C3AED", 2},
		{"Emas", "investment", &invID, "star", "#7C3AED", 3},
		{"Reksa Dana", "investment", &invID, "pie-chart", "#7C3AED", 4},
		{"Properti", "investment", &invID, "building", "#7C3AED", 5},
		{"Lain-lain (Investasi)", "investment", &invID, "circle", "#7C3AED", 6},
	}

	for _, c := range children {
		var existing string
		err := conn.QueryRowContext(ctx,
			`SELECT id FROM fin_category WHERE name=$1 AND is_system=true LIMIT 1`, c.name,
		).Scan(&existing)
		if err == nil {
			continue // already seeded
		}
		if c.parentID == nil || *c.parentID == "" {
			continue
		}
		conn.ExecContext(ctx,
			`INSERT INTO fin_category (name, type, parent_id, icon, color, is_system, display_order)
			 VALUES ($1,$2,$3,$4,$5,true,$6)
			 ON CONFLICT DO NOTHING`,
			c.name, c.typ, *c.parentID, c.icon, c.color, c.order,
		)
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
	// init balance row for the wallet
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
		`INSERT INTO fin_approval_setting (enabled, require_for_types)
		 VALUES (false, '{}')
		 ON CONFLICT DO NOTHING`,
	)
	return err
}
