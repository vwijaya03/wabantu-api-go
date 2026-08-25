package finance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/tenant"
	"encore.app/wabantu/usage"
)

var db = sqldb.Named("tenant")

// ---------- helpers ----------

func mustUser(ctx context.Context) (*types.AuthUser, error) {
	data := auth.Data()
	if data == nil {
		return nil, appErrs.Unauthenticated("not authenticated")
	}
	u, ok := data.(*types.AuthUser)
	if !ok || !u.HasEffectiveTenantContext() {
		return nil, appErrs.Forbidden("tenant context required")
	}
	return u, nil
}

func tenantConn(ctx context.Context, schema string) (*sql.Conn, error) {
	return tenant.TenantConn(ctx, schema)
}

func isOwner(u *types.AuthUser) bool { return u.CanPerformOwnerActions() }

func assertOwner(u *types.AuthUser) error {
	if !isOwner(u) {
		return appErrs.Forbidden("only owner can perform this action")
	}
	return nil
}

func moneyString(v float64) string {
	if math.Abs(v) < 0.005 {
		v = 0
	}
	return fmt.Sprintf("%.2f", v)
}

// periodLocked returns true if the given YYYY-MM period is locked.
func periodLocked(ctx context.Context, conn *sql.Conn, period string) (bool, error) {
	var exists bool
	err := conn.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM fin_period_lock WHERE period=$1)`, period,
	).Scan(&exists)
	return exists, err
}

func walletPeriod(d string) string {
	// d is YYYY-MM-DD → return YYYY-MM
	if len(d) >= 7 {
		return d[:7]
	}
	loc, _ := time.LoadLocation(defaultFinanceTimezone)
	return time.Now().In(loc).Format("2006-01")
}

// refreshWalletBalance recalculates and upserts fin_wallet_balance for one wallet.
func refreshWalletBalance(ctx context.Context, conn *sql.Conn, walletID string) error {
	const q = `
		WITH delta AS (
			SELECT
				COALESCE(SUM(CASE
					WHEN COALESCE(tt.flow,
						CASE WHEN t.type IN ('income','dividend','interest','cashback','investment_sell') THEN 'income'
						     WHEN t.type IN ('expense','investment_buy') THEN 'expense'
						     WHEN t.type = 'transfer' THEN 'transfer'
						     WHEN t.type = 'adjustment' THEN 'adjustment'
						     ELSE 'expense' END) = 'income' AND t.wallet_id = $1 THEN t.amount
					WHEN COALESCE(tt.flow,
						CASE WHEN t.type IN ('income','dividend','interest','cashback','investment_sell') THEN 'income'
						     WHEN t.type IN ('expense','investment_buy') THEN 'expense'
						     WHEN t.type = 'transfer' THEN 'transfer'
						     WHEN t.type = 'adjustment' THEN 'adjustment'
						     ELSE 'expense' END) = 'transfer' AND t.to_wallet_id = $1 THEN t.amount
					WHEN COALESCE(tt.flow,
						CASE WHEN t.type IN ('income','dividend','interest','cashback','investment_sell') THEN 'income'
						     WHEN t.type IN ('expense','investment_buy') THEN 'expense'
						     WHEN t.type = 'transfer' THEN 'transfer'
						     WHEN t.type = 'adjustment' THEN 'adjustment'
						     ELSE 'expense' END) = 'adjustment' AND t.wallet_id = $1 AND t.amount > 0 THEN t.amount
					ELSE 0 END), 0)
				- COALESCE(SUM(CASE
					WHEN COALESCE(tt.flow,
						CASE WHEN t.type IN ('income','dividend','interest','cashback','investment_sell') THEN 'income'
						     WHEN t.type IN ('expense','investment_buy') THEN 'expense'
						     WHEN t.type = 'transfer' THEN 'transfer'
						     WHEN t.type = 'adjustment' THEN 'adjustment'
						     ELSE 'expense' END) = 'expense' AND t.wallet_id = $1 THEN t.amount
					WHEN COALESCE(tt.flow,
						CASE WHEN t.type IN ('income','dividend','interest','cashback','investment_sell') THEN 'income'
						     WHEN t.type IN ('expense','investment_buy') THEN 'expense'
						     WHEN t.type = 'transfer' THEN 'transfer'
						     WHEN t.type = 'adjustment' THEN 'adjustment'
						     ELSE 'expense' END) = 'transfer' AND t.wallet_id = $1 AND t.to_wallet_id IS DISTINCT FROM $1 THEN t.amount
					WHEN COALESCE(tt.flow,
						CASE WHEN t.type IN ('income','dividend','interest','cashback','investment_sell') THEN 'income'
						     WHEN t.type IN ('expense','investment_buy') THEN 'expense'
						     WHEN t.type = 'transfer' THEN 'transfer'
						     WHEN t.type = 'adjustment' THEN 'adjustment'
						     ELSE 'expense' END) = 'adjustment' AND t.wallet_id = $1 AND t.amount < 0 THEN ABS(t.amount)
					ELSE 0 END), 0) AS net
			FROM fin_transaction t
			LEFT JOIN fin_transaction_type tt ON tt.code = t.type AND tt.deleted_at IS NULL
			WHERE (t.wallet_id = $1 OR t.to_wallet_id = $1)
			  AND t.status = 'approved'
			  AND t.deleted_at IS NULL
		),
		init AS (SELECT COALESCE(initial_balance,0) AS ib FROM fin_wallet WHERE id=$1)
		INSERT INTO fin_wallet_balance (wallet_id, balance, computed_at)
		SELECT $1, init.ib + delta.net, now() FROM delta, init
		ON CONFLICT (wallet_id) DO UPDATE
		SET balance = EXCLUDED.balance, computed_at = EXCLUDED.computed_at`
	_, err := conn.ExecContext(ctx, q, walletID)
	return err
}

// auditFinance writes a finance audit record.
func auditFinance(ctx context.Context, conn *sql.Conn, u *types.AuthUser, entityType, entityID, action string, before, after any) {
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	conn.ExecContext(ctx,
		`INSERT INTO fin_audit_log (entity_type, entity_id, action, actor_id, actor_role, before_data, after_data)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		entityType, entityID, action, u.AccountID, u.Role, beforeJSON, afterJSON,
	)
}

type OKResponse struct {
	OK bool `json:"ok"`
}

type CountResponse struct {
	Count int `json:"count"`
}

type LockedPeriodsResponse struct {
	Periods []string `json:"periods"`
}

// ============================================================
// WALLET
// ============================================================

type Wallet struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	Institution    *string   `json:"institution,omitempty"`
	AccountNoMask  *string   `json:"accountNoMask,omitempty"`
	Currency       string    `json:"currency"`
	InitialBalance string    `json:"initialBalance"`
	Color          *string   `json:"color,omitempty"`
	Icon           *string   `json:"icon,omitempty"`
	IsActive       bool      `json:"isActive"`
	Visibility     string    `json:"visibility"`
	DisplayOrder   int       `json:"displayOrder"`
	Balance        string    `json:"balance"`
	CreatedAt      time.Time `json:"createdAt"`
}

type WalletListResponse struct {
	Wallets []Wallet `json:"wallets"`
}

type CreateWalletParams struct {
	Name           string  `json:"name"`
	Type           string  `json:"type"`
	Institution    *string `json:"institution,omitempty"`
	AccountNo      *string `json:"accountNo,omitempty"`
	Currency       string  `json:"currency"`
	InitialBalance float64 `json:"initialBalance"`
	Color          *string `json:"color,omitempty"`
	Icon           *string `json:"icon,omitempty"`
	Visibility     string  `json:"visibility"`
	DisplayOrder   int     `json:"displayOrder"`
}

type UpdateWalletParams struct {
	ID           string  `json:"id"`
	Name         *string `json:"name,omitempty"`
	Type         *string `json:"type,omitempty"`
	Institution  *string `json:"institution,omitempty"`
	Color        *string `json:"color,omitempty"`
	Icon         *string `json:"icon,omitempty"`
	Visibility   *string `json:"visibility,omitempty"`
	IsActive     *bool   `json:"isActive,omitempty"`
	DisplayOrder *int    `json:"displayOrder,omitempty"`
}

var validWalletTypes = map[string]bool{"cash": true, "bank": true, "ewallet": true, "crypto": true, "investment": true, "other": true}

//encore:api auth method=GET path=/api/v1/finance/wallets
func ListWallets(ctx context.Context) (*WalletListResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	ownerOnly := ""
	if !isOwner(u) {
		ownerOnly = " AND w.visibility = 'all'"
	}

	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT w.id, w.name, w.type, w.institution, w.account_no, w.currency,
		       w.initial_balance, w.color, w.icon, w.is_active, w.visibility,
		       w.display_order, COALESCE(b.balance, w.initial_balance), w.created_at
		FROM fin_wallet w
		LEFT JOIN fin_wallet_balance b ON b.wallet_id = w.id
		WHERE w.deleted_at IS NULL %s
		ORDER BY w.display_order, w.created_at`, ownerOnly))
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var wallets []Wallet
	for rows.Next() {
		var w Wallet
		var institution, accountNo, color, icon sql.NullString
		var initBal, bal float64
		if err := rows.Scan(&w.ID, &w.Name, &w.Type, &institution, &accountNo,
			&w.Currency, &initBal, &color, &icon, &w.IsActive,
			&w.Visibility, &w.DisplayOrder, &bal, &w.CreatedAt); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if institution.Valid {
			w.Institution = &institution.String
		}
		if accountNo.Valid {
			// mask account number: show last 4 digits only
			masked := maskAccountNo(accountNo.String)
			w.AccountNoMask = &masked
		}
		if color.Valid {
			w.Color = &color.String
		}
		if icon.Valid {
			w.Icon = &icon.String
		}
		w.InitialBalance = fmt.Sprintf("%.2f", initBal)
		w.Balance = fmt.Sprintf("%.2f", bal)
		wallets = append(wallets, w)
	}
	if wallets == nil {
		wallets = []Wallet{}
	}
	return &WalletListResponse{Wallets: wallets}, nil
}

func maskAccountNo(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("•", len(s))
	}
	return strings.Repeat("•", len(s)-4) + s[len(s)-4:]
}

//encore:api auth method=POST path=/api/v1/finance/wallets
func CreateWallet(ctx context.Context, p *CreateWalletParams) (*Wallet, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, appErrs.BadRequest("nama wallet tidak boleh kosong")
	}
	if !validWalletTypes[p.Type] {
		return nil, appErrs.BadRequest("tipe wallet tidak valid")
	}
	if p.Currency == "" {
		p.Currency = "IDR"
	}
	if p.Visibility != "owner" {
		p.Visibility = "all"
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	var id string
	err = conn.QueryRowContext(ctx,
		`INSERT INTO fin_wallet (name,type,institution,account_no,currency,initial_balance,color,icon,is_active,visibility,display_order,created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,true,$9,$10,$11) RETURNING id`,
		strings.TrimSpace(p.Name), p.Type, p.Institution, p.AccountNo,
		p.Currency, p.InitialBalance, p.Color, p.Icon, p.Visibility, p.DisplayOrder, u.AccountID,
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	conn.ExecContext(ctx,
		`INSERT INTO fin_wallet_balance (wallet_id, balance) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		id, p.InitialBalance,
	)

	auditFinance(ctx, conn, u, "wallet", id, "create", nil, p)
	w := &Wallet{ID: id, Name: p.Name, Type: p.Type, Currency: p.Currency,
		InitialBalance: fmt.Sprintf("%.2f", p.InitialBalance),
		Balance:        fmt.Sprintf("%.2f", p.InitialBalance),
		IsActive:       true, Visibility: p.Visibility, DisplayOrder: p.DisplayOrder,
		Color: p.Color, Icon: p.Icon, Institution: p.Institution, CreatedAt: time.Now()}
	return w, nil
}

//encore:api auth method=PUT path=/api/v1/finance/wallets/:id
func UpdateWallet(ctx context.Context, id string, p *UpdateWalletParams) (*Wallet, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	sets := []string{"updated_at=now()"}
	args := []any{}
	i := 1
	add := func(col string, v any) {
		sets = append(sets, fmt.Sprintf("%s=$%d", col, i))
		args = append(args, v)
		i++
	}
	if p.Name != nil {
		add("name", strings.TrimSpace(*p.Name))
	}
	if p.Type != nil {
		if !validWalletTypes[*p.Type] {
			return nil, appErrs.BadRequest("tipe wallet tidak valid")
		}
		add("type", *p.Type)
	}
	if p.Institution != nil {
		add("institution", *p.Institution)
	}
	if p.Color != nil {
		add("color", *p.Color)
	}
	if p.Icon != nil {
		add("icon", *p.Icon)
	}
	if p.Visibility != nil {
		add("visibility", *p.Visibility)
	}
	if p.IsActive != nil {
		add("is_active", *p.IsActive)
	}
	if p.DisplayOrder != nil {
		add("display_order", *p.DisplayOrder)
	}
	if len(sets) == 1 {
		return nil, appErrs.BadRequest("tidak ada perubahan")
	}
	args = append(args, id)
	_, err = conn.ExecContext(ctx,
		fmt.Sprintf(`UPDATE fin_wallet SET %s WHERE id=$%d AND deleted_at IS NULL`,
			strings.Join(sets, ","), i), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditFinance(ctx, conn, u, "wallet", id, "edit", nil, p)
	return &Wallet{ID: id}, nil
}

func walletDeleteBlocked(ctx context.Context, conn *sql.Conn, walletID string) error {
	var txnCount, assetCount, recurringCount int
	_ = conn.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM fin_transaction
		WHERE deleted_at IS NULL AND (wallet_id=$1 OR to_wallet_id=$1)`, walletID,
	).Scan(&txnCount)
	if finAssetTableReady(ctx, conn) {
		_ = conn.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM fin_asset WHERE wallet_id=$1 AND is_active=true`, walletID,
		).Scan(&assetCount)
	}
	_ = conn.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM fin_recurring
		WHERE deleted_at IS NULL AND (wallet_id=$1 OR to_wallet_id=$1)`, walletID,
	).Scan(&recurringCount)

	if txnCount > 0 {
		return appErrs.BadRequest("dompet masih memiliki transaksi. Hapus atau pindahkan transaksi terlebih dahulu.")
	}
	if assetCount > 0 {
		return appErrs.BadRequest("dompet masih terhubung ke aset investasi aktif. Ubah dompet aset atau hapus aset terlebih dahulu.")
	}
	if recurringCount > 0 {
		return appErrs.BadRequest("dompet masih dipakai transaksi berulang. Hapus transaksi berulang terlebih dahulu.")
	}
	return nil
}

//encore:api auth method=DELETE path=/api/v1/finance/wallets/:id
func DeleteWallet(ctx context.Context, id string) (*OKResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, appErrs.BadRequest("dompet tidak valid")
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	var exists bool
	if err := conn.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM fin_wallet WHERE id=$1 AND deleted_at IS NULL)`, id,
	).Scan(&exists); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if !exists {
		return nil, appErrs.NotFound("dompet tidak ditemukan")
	}

	if err := walletDeleteBlocked(ctx, conn, id); err != nil {
		return nil, err
	}

	res, err := conn.ExecContext(ctx,
		`UPDATE fin_wallet SET deleted_at=now() WHERE id=$1 AND deleted_at IS NULL`, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, appErrs.NotFound("dompet tidak ditemukan")
	}
	auditFinance(ctx, conn, u, "wallet", id, "delete", nil, nil)
	return &OKResponse{OK: true}, nil
}

// ============================================================
// CATEGORY
// ============================================================

type Category struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	ParentID     *string   `json:"parentId,omitempty"`
	Icon         *string   `json:"icon,omitempty"`
	Color        *string   `json:"color,omitempty"`
	IsSystem     bool      `json:"isSystem"`
	DisplayOrder int       `json:"displayOrder"`
	CreatedAt    time.Time `json:"createdAt"`
}

type CategoryListResponse struct {
	Categories []Category `json:"categories"`
}

type CreateCategoryParams struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	ParentID     *string `json:"parentId,omitempty"`
	Icon         *string `json:"icon,omitempty"`
	Color        *string `json:"color,omitempty"`
	DisplayOrder int     `json:"displayOrder"`
}

//encore:api auth method=GET path=/api/v1/finance/categories
func ListCategories(ctx context.Context) (*CategoryListResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	rows, err := conn.QueryContext(ctx,
		`SELECT id, name, type, parent_id, icon, color, is_system, display_order, created_at
		 FROM fin_category WHERE deleted_at IS NULL ORDER BY display_order, name`)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var cats []Category
	for rows.Next() {
		var c Category
		var parentID, icon, color sql.NullString
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &parentID, &icon, &color,
			&c.IsSystem, &c.DisplayOrder, &c.CreatedAt); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if parentID.Valid {
			c.ParentID = &parentID.String
		}
		if icon.Valid {
			c.Icon = &icon.String
		}
		if color.Valid {
			c.Color = &color.String
		}
		cats = append(cats, c)
	}
	if cats == nil {
		cats = []Category{}
	}
	return &CategoryListResponse{Categories: cats}, nil
}

//encore:api auth method=POST path=/api/v1/finance/categories
func CreateCategory(ctx context.Context, p *CreateCategoryParams) (*Category, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, appErrs.BadRequest("nama kategori tidak boleh kosong")
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	var id string
	err = conn.QueryRowContext(ctx,
		`INSERT INTO fin_category (name,type,parent_id,icon,color,is_system,display_order)
		 VALUES ($1,$2,$3,$4,$5,false,$6) RETURNING id`,
		strings.TrimSpace(p.Name), p.Type, p.ParentID, p.Icon, p.Color, p.DisplayOrder,
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &Category{ID: id, Name: p.Name, Type: p.Type, ParentID: p.ParentID,
		Icon: p.Icon, Color: p.Color, DisplayOrder: p.DisplayOrder, CreatedAt: time.Now()}, nil
}

//encore:api auth method=DELETE path=/api/v1/finance/categories/:id
func DeleteCategory(ctx context.Context, id string) (*OKResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)
	var isSystem bool
	if err := conn.QueryRowContext(ctx,
		`SELECT is_system FROM fin_category WHERE id=$1 AND deleted_at IS NULL`, id,
	).Scan(&isSystem); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, appErrs.NotFound("kategori tidak ditemukan")
		}
		return nil, appErrs.Internal(err.Error())
	}
	if isSystem {
		return nil, appErrs.BadRequest("kategori bawaan tidak bisa dihapus")
	}
	res, err := conn.ExecContext(ctx, `UPDATE fin_category SET deleted_at=now() WHERE id=$1 AND deleted_at IS NULL`, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, appErrs.NotFound("kategori tidak ditemukan")
	}
	return &OKResponse{OK: true}, nil
}

// ============================================================
// TRANSACTION
// ============================================================

type Transaction struct {
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	Amount            string          `json:"amount"`
	Currency          string          `json:"currency"`
	WalletID          string          `json:"walletId"`
	WalletName        string          `json:"walletName"`
	ToWalletID        *string         `json:"toWalletId,omitempty"`
	ToWalletName      *string         `json:"toWalletName,omitempty"`
	CategoryID        *string         `json:"categoryId,omitempty"`
	CategoryName      *string         `json:"categoryName,omitempty"`
	Description       *string         `json:"description,omitempty"`
	Notes             *string         `json:"notes,omitempty"`
	ReferenceNo       *string         `json:"referenceNo,omitempty"`
	TransactionDate   string          `json:"transactionDate"`
	Status            string          `json:"status"`
	Tags              []string        `json:"tags"`
	AttachmentURLs    json.RawMessage `json:"attachmentUrls"`
	AssetID           *string         `json:"assetId,omitempty"`
	AssetName         *string         `json:"assetName,omitempty"`
	AssetTicker       *string         `json:"assetTicker,omitempty"`
	AssetQty          *string         `json:"assetQty,omitempty"`
	AssetPricePerUnit *string         `json:"assetPricePerUnit,omitempty"`
	CreatedBy         string          `json:"createdBy"`
	CreatedAt         time.Time       `json:"createdAt"`
}

type ListTransactionsParams struct {
	WalletID   string `query:"walletId"`
	CategoryID string `query:"categoryId"`
	Type       string `query:"type"`
	Status     string `query:"status"`
	Search     string `query:"search"`
	StartDate  string `query:"startDate"`
	EndDate    string `query:"endDate"`
	Period     string `query:"period"` // YYYY-MM shortcut
	Page       int    `query:"page"`
	PageSize   int    `query:"pageSize"`
}

type ListTransactionsResponse struct {
	Items []Transaction `json:"items"`
	Total int           `json:"total"`
}

type CreateTransactionParams struct {
	Type            string   `json:"type"`
	Amount          float64  `json:"amount"`
	Currency        string   `json:"currency"`
	WalletID        string   `json:"walletId"`
	ToWalletID      *string  `json:"toWalletId,omitempty"`
	CategoryID      *string  `json:"categoryId,omitempty"`
	Description     *string  `json:"description,omitempty"`
	Notes           *string  `json:"notes,omitempty"`
	ReferenceNo     *string  `json:"referenceNo,omitempty"`
	TransactionDate string   `json:"transactionDate"`
	Tags            []string `json:"tags"`
}

type UpdateTransactionParams struct {
	CategoryID      *string  `json:"categoryId,omitempty"`
	Description     *string  `json:"description,omitempty"`
	Notes           *string  `json:"notes,omitempty"`
	ReferenceNo     *string  `json:"referenceNo,omitempty"`
	TransactionDate *string  `json:"transactionDate,omitempty"`
	Amount          *float64 `json:"amount,omitempty"`
	Tags            []string `json:"tags,omitempty"`
}

func validateCreateTransaction(p *CreateTransactionParams) error {
	if strings.TrimSpace(p.Type) == "" {
		return appErrs.BadRequest("jenis transaksi tidak valid")
	}
	if p.Amount <= 0 {
		return appErrs.BadRequest("jumlah harus lebih dari 0")
	}
	if p.Amount > 999_999_999_999 {
		return appErrs.BadRequest("jumlah terlalu besar")
	}
	if p.WalletID == "" {
		return appErrs.BadRequest("wallet harus dipilih")
	}
	if p.Type == "transfer" && (p.ToWalletID == nil || *p.ToWalletID == "") {
		return appErrs.BadRequest("transfer harus ada wallet tujuan")
	}
	if p.Type == "transfer" && p.ToWalletID != nil && *p.ToWalletID == p.WalletID {
		return appErrs.BadRequest("wallet asal dan tujuan tidak boleh sama")
	}
	if p.Currency == "" {
		p.Currency = "IDR"
	}
	return nil
}

//encore:api auth method=GET path=/api/v1/finance/transactions
func ListTransactions(ctx context.Context, p *ListTransactionsParams) (*ListTransactionsResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	if p.Page <= 0 {
		p.Page = 1
	}
	if p.PageSize <= 0 || p.PageSize > 100 {
		p.PageSize = 50
	}

	conditions := []string{"t.deleted_at IS NULL"}
	args := []any{}
	i := 1

	if !isOwner(u) {
		conditions = append(conditions, `t.status IN ('approved','pending_approval')`)
		conditions = append(conditions, staffTxnVisibilitySQL()[5:]) // trim leading " AND "
	}
	if p.WalletID != "" {
		conditions = append(conditions, fmt.Sprintf("(t.wallet_id=$%d OR t.to_wallet_id=$%d)", i, i+1))
		args = append(args, p.WalletID, p.WalletID)
		i += 2
	}
	if s := strings.TrimSpace(p.Search); s != "" {
		like := "%" + s + "%"
		conditions = append(conditions, fmt.Sprintf(`(
			COALESCE(t.description,'') ILIKE $%d OR
			COALESCE(t.notes,'') ILIKE $%d OR
			COALESCE(t.reference_no,'') ILIKE $%d OR
			COALESCE(w.name,'') ILIKE $%d OR
			COALESCE(c.name,'') ILIKE $%d OR
			COALESCE(a.name,'') ILIKE $%d OR
			COALESCE(a.ticker,'') ILIKE $%d OR
			t.type ILIKE $%d OR
			CAST(t.amount AS TEXT) ILIKE $%d
		)`, i, i, i, i, i, i, i, i, i))
		for j := 0; j < 9; j++ {
			args = append(args, like)
		}
		i += 9
	}
	if p.CategoryID != "" {
		conditions = append(conditions, fmt.Sprintf("t.category_id=$%d", i))
		args = append(args, p.CategoryID)
		i++
	}
	if p.Type != "" {
		conditions = append(conditions, fmt.Sprintf("t.type=$%d", i))
		args = append(args, p.Type)
		i++
	}
	if p.Status != "" {
		conditions = append(conditions, fmt.Sprintf("t.status=$%d", i))
		args = append(args, p.Status)
		i++
	}
	if p.Period != "" {
		conditions = append(conditions, fmt.Sprintf("to_char(t.transaction_date,'YYYY-MM')=$%d", i))
		args = append(args, p.Period)
		i++
	} else {
		if p.StartDate != "" {
			conditions = append(conditions, fmt.Sprintf("t.transaction_date>=$%d", i))
			args = append(args, p.StartDate)
			i++
		}
		if p.EndDate != "" {
			conditions = append(conditions, fmt.Sprintf("t.transaction_date<=$%d", i))
			args = append(args, p.EndDate)
			i++
		}
	}

	where := strings.Join(conditions, " AND ")
	offset := (p.Page - 1) * p.PageSize

	countQ := fmt.Sprintf(`SELECT COUNT(*) FROM fin_transaction t
		LEFT JOIN fin_wallet w ON w.id = t.wallet_id
		LEFT JOIN fin_wallet tw ON tw.id = t.to_wallet_id
		LEFT JOIN fin_category c ON c.id = t.category_id
		LEFT JOIN fin_asset a ON a.id = t.asset_id
		WHERE %s`, where)
	var total int
	conn.QueryRowContext(ctx, countQ, args...).Scan(&total)

	args = append(args, p.PageSize, offset)
	dataQ := fmt.Sprintf(`
		SELECT t.id, t.type, t.amount, t.currency, t.wallet_id,
		       COALESCE(w.name,''), t.to_wallet_id, tw.name,
		       t.category_id, c.name,
		       t.description, t.notes, t.reference_no,
		       t.transaction_date::text, t.status, t.tags, t.attachment_urls,
		       t.asset_id, a.name, a.ticker, t.asset_qty, t.asset_price_per_unit,
		       t.created_by, t.created_at
		FROM fin_transaction t
		LEFT JOIN fin_wallet w  ON w.id = t.wallet_id
		LEFT JOIN fin_wallet tw ON tw.id = t.to_wallet_id
		LEFT JOIN fin_category c ON c.id = t.category_id
		LEFT JOIN fin_asset a ON a.id = t.asset_id
		WHERE %s
		ORDER BY t.transaction_date DESC, t.created_at DESC
		LIMIT $%d OFFSET $%d`, where, i, i+1)

	rows, err := conn.QueryContext(ctx, dataQ, args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var items []Transaction
	for rows.Next() {
		var tx Transaction
		var toWalletID, toWalletName, categoryID, categoryName sql.NullString
		var description, notes, referenceNo sql.NullString
		var assetID, assetName, assetTicker sql.NullString
		var assetQty, assetPrice sql.NullFloat64
		var amount float64
		var tagsRaw sql.NullString
		var attachments json.RawMessage
		if err := rows.Scan(
			&tx.ID, &tx.Type, &amount, &tx.Currency, &tx.WalletID,
			&tx.WalletName, &toWalletID, &toWalletName,
			&categoryID, &categoryName,
			&description, &notes, &referenceNo,
			&tx.TransactionDate, &tx.Status, &tagsRaw, &attachments,
			&assetID, &assetName, &assetTicker, &assetQty, &assetPrice,
			&tx.CreatedBy, &tx.CreatedAt,
		); err != nil {
			rlog.Warn("scan transaction row", "err", err)
			continue
		}
		tx.Amount = fmt.Sprintf("%.2f", amount)
		if toWalletID.Valid {
			tx.ToWalletID = &toWalletID.String
		}
		if toWalletName.Valid {
			tx.ToWalletName = &toWalletName.String
		}
		if categoryID.Valid {
			tx.CategoryID = &categoryID.String
		}
		if categoryName.Valid {
			tx.CategoryName = &categoryName.String
		}
		if description.Valid {
			tx.Description = &description.String
		}
		if notes.Valid {
			tx.Notes = &notes.String
		}
		if referenceNo.Valid {
			tx.ReferenceNo = &referenceNo.String
		}
		if assetID.Valid {
			tx.AssetID = &assetID.String
		}
		if assetName.Valid {
			tx.AssetName = &assetName.String
		}
		if assetTicker.Valid {
			tx.AssetTicker = &assetTicker.String
		}
		if assetQty.Valid {
			s := fmt.Sprintf("%.6f", assetQty.Float64)
			tx.AssetQty = &s
		}
		if assetPrice.Valid {
			s := fmt.Sprintf("%.4f", assetPrice.Float64)
			tx.AssetPricePerUnit = &s
		}
		tx.Tags = parsePostgreSQLStringArray(tagsRaw)
		tx.AttachmentURLs = attachments
		if tx.AttachmentURLs == nil {
			tx.AttachmentURLs = json.RawMessage("[]")
		}
		items = append(items, tx)
	}
	if items == nil {
		items = []Transaction{}
	}
	return &ListTransactionsResponse{Items: items, Total: total}, nil
}

//encore:api auth method=POST path=/api/v1/finance/transactions
func CreateTransaction(ctx context.Context, p *CreateTransactionParams) (*Transaction, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateCreateTransaction(p); err != nil {
		return nil, err
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	if p.TransactionDate == "" {
		p.TransactionDate = financeToday(ctx, conn)
	}

	txnType, err := loadTransactionTypeByCode(ctx, conn, p.Type)
	if err != nil {
		return nil, err
	}
	if txnType.OwnerOnly {
		if err := assertOwner(u); err != nil {
			return nil, err
		}
	}
	if err := assertWalletAccessible(ctx, conn, u, p.WalletID); err != nil {
		return nil, err
	}
	if p.ToWalletID != nil && *p.ToWalletID != "" {
		if err := assertWalletAccessible(ctx, conn, u, *p.ToWalletID); err != nil {
			return nil, err
		}
	}

	status := "approved"
	if !isOwner(u) {
		cfg, err := loadApprovalConfig(ctx, conn)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if staffNeedsApproval(cfg, p.Type, p.Amount) {
			status = "pending_approval"
		}
	}

	period := walletPeriod(p.TransactionDate)
	if err := ensurePeriodUnlocked(ctx, conn, period); err != nil {
		return nil, err
	}

	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}

	dbTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer dbTx.Rollback()

	var id string
	err = dbTx.QueryRowContext(ctx,
		`INSERT INTO fin_transaction
		 (type,amount,currency,wallet_id,to_wallet_id,category_id,description,notes,
		  reference_no,transaction_date,status,tags,created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		 RETURNING id`,
		p.Type, p.Amount, p.Currency, p.WalletID, p.ToWalletID, p.CategoryID,
		p.Description, p.Notes, p.ReferenceNo, p.TransactionDate,
		status, tags, u.AccountID,
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	if err := dbTx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	if status == "approved" {
		refreshWallets(ctx, conn, p.WalletID, p.ToWalletID)
	}

	// Record usage event
	usage.RecordEvent(ctx, u.TenantSchema, "fin_transaction_created", 1, nil)

	auditFinance(ctx, conn, u, "transaction", id, "create", nil, p)

	return &Transaction{
		ID: id, Type: p.Type,
		Amount:   fmt.Sprintf("%.2f", p.Amount),
		Currency: p.Currency, WalletID: p.WalletID,
		ToWalletID: p.ToWalletID, CategoryID: p.CategoryID,
		TransactionDate: p.TransactionDate, Status: status,
		Tags: tags, AttachmentURLs: json.RawMessage("[]"),
		CreatedBy: u.AccountID, CreatedAt: time.Now(),
	}, nil
}

//encore:api auth method=PUT path=/api/v1/finance/transactions/:id
func UpdateTransaction(ctx context.Context, id string, p *UpdateTransactionParams) (*Transaction, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	// Fetch existing to check ownership and period lock
	var txType, txDate, txStatus, createdBy string
	if err := conn.QueryRowContext(ctx,
		`SELECT type, transaction_date::text, status, created_by FROM fin_transaction
		 WHERE id=$1 AND deleted_at IS NULL`, id,
	).Scan(&txType, &txDate, &txStatus, &createdBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, appErrs.NotFound("transaksi tidak ditemukan")
		}
		return nil, appErrs.Internal(err.Error())
	}

	// Staff can only edit their own draft/pending
	if !isOwner(u) {
		if createdBy != u.AccountID {
			return nil, appErrs.Forbidden("hanya bisa edit transaksi milik sendiri")
		}
		if txStatus != "draft" && txStatus != "pending_approval" && txStatus != "rejected" {
			return nil, appErrs.Forbidden("transaksi sudah diproses, tidak bisa diedit")
		}
	}

	period := walletPeriod(txDate)
	if err := ensurePeriodUnlocked(ctx, conn, period); err != nil {
		return nil, err
	}
	if p.TransactionDate != nil {
		newPeriod := walletPeriod(*p.TransactionDate)
		if err := ensurePeriodUnlocked(ctx, conn, newPeriod); err != nil {
			return nil, err
		}
	}

	sets := []string{"updated_at=now()"}
	args := []any{}
	i := 1
	add := func(col string, v any) {
		sets = append(sets, fmt.Sprintf("%s=$%d", col, i))
		args = append(args, v)
		i++
	}
	if p.CategoryID != nil {
		add("category_id", *p.CategoryID)
	}
	if p.Description != nil {
		add("description", *p.Description)
	}
	if p.Notes != nil {
		add("notes", *p.Notes)
	}
	if p.ReferenceNo != nil {
		add("reference_no", *p.ReferenceNo)
	}
	if p.TransactionDate != nil {
		add("transaction_date", *p.TransactionDate)
	}
	if p.Amount != nil && isOwner(u) {
		if *p.Amount <= 0 {
			return nil, appErrs.BadRequest("jumlah harus lebih dari 0")
		}
		add("amount", *p.Amount)
	}
	if p.Tags != nil {
		add("tags", p.Tags)
	}

	args = append(args, id)
	_, err = conn.ExecContext(ctx,
		fmt.Sprintf(`UPDATE fin_transaction SET %s WHERE id=$%d`, strings.Join(sets, ","), i),
		args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	if (p.Amount != nil || p.TransactionDate != nil) && txStatus == "approved" {
		var walletID string
		var toWalletID sql.NullString
		conn.QueryRowContext(ctx,
			`SELECT wallet_id, to_wallet_id FROM fin_transaction WHERE id=$1`, id,
		).Scan(&walletID, &toWalletID)
		var toPtr *string
		if toWalletID.Valid && toWalletID.String != "" {
			toPtr = &toWalletID.String
		}
		refreshWallets(ctx, conn, walletID, toPtr)
	}

	auditFinance(ctx, conn, u, "transaction", id, "edit", txStatus, p)
	return &Transaction{ID: id, Type: txType, Status: txStatus}, nil
}

//encore:api auth method=DELETE path=/api/v1/finance/transactions/:id
func DeleteTransaction(ctx context.Context, id string) (*OKResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	var walletID, txDate string
	var toWalletID sql.NullString
	if err := conn.QueryRowContext(ctx,
		`SELECT wallet_id, to_wallet_id, transaction_date::text FROM fin_transaction WHERE id=$1 AND deleted_at IS NULL`, id,
	).Scan(&walletID, &toWalletID, &txDate); err != nil {
		return nil, appErrs.NotFound("transaksi tidak ditemukan")
	}

	if err := ensurePeriodUnlocked(ctx, conn, walletPeriod(txDate)); err != nil {
		return nil, err
	}

	conn.ExecContext(ctx, `DELETE FROM fin_transaction WHERE id=$1`, id)
	var toPtr *string
	if toWalletID.Valid && toWalletID.String != "" {
		toPtr = &toWalletID.String
	}
	refreshWallets(ctx, conn, walletID, toPtr)
	auditFinance(ctx, conn, u, "transaction", id, "delete", nil, nil)
	return &OKResponse{OK: true}, nil
}

// ============================================================
// APPROVAL
// ============================================================

type ApproveParams struct {
	ID     string `json:"id"`
	Action string `json:"action"` // "approve" | "reject"
	Reason string `json:"reason,omitempty"`
}

//encore:api auth method=POST path=/api/v1/finance/transactions/approve
func ApproveTransaction(ctx context.Context, p *ApproveParams) (*Transaction, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if p.Action != "approve" && p.Action != "reject" {
		return nil, appErrs.BadRequest("action harus approve atau reject")
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	var walletID, curStatus string
	var toWalletID sql.NullString
	if err := conn.QueryRowContext(ctx,
		`SELECT wallet_id, to_wallet_id, status FROM fin_transaction WHERE id=$1 AND deleted_at IS NULL`, p.ID,
	).Scan(&walletID, &toWalletID, &curStatus); err != nil {
		return nil, appErrs.NotFound("transaksi tidak ditemukan")
	}
	if curStatus != "pending_approval" {
		return nil, appErrs.BadRequest("transaksi tidak dalam status menunggu persetujuan")
	}

	newStatus := "approved"
	if p.Action == "reject" {
		newStatus = "rejected"
	}
	_, err = conn.ExecContext(ctx,
		`UPDATE fin_transaction SET status=$1, approved_by=$2, approved_at=now(),
		 rejected_reason=CASE WHEN $1='rejected' THEN $3 ELSE NULL END,
		 updated_at=now()
		 WHERE id=$4`,
		newStatus, u.AccountID, p.Reason, p.ID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if newStatus == "approved" {
		var toPtr *string
		if toWalletID.Valid && toWalletID.String != "" {
			toPtr = &toWalletID.String
		}
		refreshWallets(ctx, conn, walletID, toPtr)
	}
	auditFinance(ctx, conn, u, "transaction", p.ID, p.Action, curStatus, newStatus)
	return &Transaction{ID: p.ID, Status: newStatus}, nil
}

type ApprovalSettingParams struct {
	Enabled         bool     `json:"enabled"`
	AmountThreshold *float64 `json:"amountThreshold,omitempty"`
	RequireForTypes []string `json:"requireForTypes"`
}

//encore:api auth method=GET path=/api/v1/finance/approval-setting
func GetApprovalSetting(ctx context.Context) (*ApprovalSettingParams, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)
	var s ApprovalSettingParams
	var threshold sql.NullFloat64
	conn.QueryRowContext(ctx,
		`SELECT enabled, amount_threshold, require_for_types FROM fin_approval_setting WHERE id=$1`,
		approvalSettingID,
	).Scan(&s.Enabled, &threshold, &s.RequireForTypes)
	if threshold.Valid {
		s.AmountThreshold = &threshold.Float64
	}
	if s.RequireForTypes == nil {
		s.RequireForTypes = []string{}
	}
	return &s, nil
}

//encore:api auth method=PUT path=/api/v1/finance/approval-setting
func UpdateApprovalSetting(ctx context.Context, p *ApprovalSettingParams) (*ApprovalSettingParams, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)
	types := p.RequireForTypes
	if types == nil {
		types = []string{}
	}
	_, err = conn.ExecContext(ctx,
		`INSERT INTO fin_approval_setting (id, enabled, amount_threshold, require_for_types, updated_by)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (id) DO UPDATE SET
		   enabled=EXCLUDED.enabled,
		   amount_threshold=EXCLUDED.amount_threshold,
		   require_for_types=EXCLUDED.require_for_types,
		   updated_by=EXCLUDED.updated_by,
		   updated_at=now()`,
		approvalSettingID, p.Enabled, p.AmountThreshold, types, u.AccountID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return p, nil
}

// ============================================================
// PERIOD LOCK
// ============================================================

type LockPeriodParams struct {
	Period string `json:"period"` // YYYY-MM
	Note   string `json:"note,omitempty"`
}

//encore:api auth method=POST path=/api/v1/finance/period-lock
func LockPeriod(ctx context.Context, p *LockPeriodParams) (*OKResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)
	_, err = conn.ExecContext(ctx,
		`INSERT INTO fin_period_lock (period, locked_by, note) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
		p.Period, u.AccountID, p.Note)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditFinance(ctx, conn, u, "period_lock", u.TenantID, "lock_period", nil, p)
	return &OKResponse{OK: true}, nil
}

//encore:api auth method=GET path=/api/v1/finance/locked-periods
func ListLockedPeriods(ctx context.Context) (*LockedPeriodsResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)
	rows, err := conn.QueryContext(ctx, `SELECT period FROM fin_period_lock ORDER BY period DESC`)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	var ps []string
	for rows.Next() {
		var p string
		rows.Scan(&p)
		ps = append(ps, p)
	}
	if ps == nil {
		ps = []string{}
	}
	return &LockedPeriodsResponse{Periods: ps}, nil
}

// ============================================================
// DUPLICATE TRANSACTION
// ============================================================

type DuplicateParams struct {
	TransactionIDs []string `json:"transactionIds"`
	TargetDate     string   `json:"targetDate"`
}

//encore:api auth method=POST path=/api/v1/finance/transactions/duplicate
func DuplicateTransactions(ctx context.Context, p *DuplicateParams) (*CountResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if len(p.TransactionIDs) == 0 {
		return nil, appErrs.BadRequest("pilih minimal satu transaksi")
	}
	if p.TargetDate == "" {
		return nil, appErrs.BadRequest("tanggal target harus diisi")
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	if err := ensurePeriodUnlocked(ctx, conn, walletPeriod(p.TargetDate)); err != nil {
		return nil, err
	}

	count := 0
	for _, txID := range p.TransactionIDs {
		var newID string
		err := conn.QueryRowContext(ctx,
			`INSERT INTO fin_transaction
			 (type,amount,currency,wallet_id,to_wallet_id,category_id,description,notes,
			  tags,status,transaction_date,created_by)
			 SELECT type,amount,currency,wallet_id,to_wallet_id,category_id,description,notes,
			        tags,'approved',$1,$2
			 FROM fin_transaction WHERE id=$3 AND deleted_at IS NULL
			 RETURNING id`,
			p.TargetDate, u.AccountID, txID,
		).Scan(&newID)
		if err != nil {
			rlog.Warn("duplicate txn failed", "src", txID, "err", err)
			continue
		}
		refreshWalletsForTransaction(ctx, conn, newID)
		count++
	}
	return &CountResponse{Count: count}, nil
}

// ============================================================
// DASHBOARD SUMMARY
// ============================================================

type DashboardSummary struct {
	Period       string       `json:"period"`
	TotalIncome  string       `json:"totalIncome"`
	TotalExpense string       `json:"totalExpense"`
	NetBalance   string       `json:"netBalance"`
	TotalWallets string       `json:"totalWallets"`
	PendingCount int          `json:"pendingCount"`
	Wallets      []WalletSnap `json:"wallets"`
}

type WalletSnap struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Balance  string  `json:"balance"`
	Currency string  `json:"currency"`
	Color    *string `json:"color,omitempty"`
	Icon     *string `json:"icon,omitempty"`
}

type DashboardParams struct {
	Period string `query:"period"` // YYYY-MM, default current month
}

//encore:api auth method=GET path=/api/v1/finance/dashboard
func GetDashboard(ctx context.Context, p *DashboardParams) (*DashboardSummary, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	period := p.Period
	if period == "" {
		period = financePeriod(ctx, conn)
	}

	flowSQL := flowFallbackSQL("t.type")
	walletFilter := ""
	if !isOwner(u) {
		walletFilter = staffWalletBalanceFilter()
	}
	var income, expense float64
	conn.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT
		  COALESCE(SUM(CASE WHEN %s = 'income' THEN t.amount ELSE 0 END),0),
		  COALESCE(SUM(CASE WHEN %s = 'expense' THEN t.amount ELSE 0 END),0)
		 FROM fin_transaction t
		 LEFT JOIN fin_transaction_type tt ON tt.code = t.type AND tt.deleted_at IS NULL
		 JOIN fin_wallet w ON w.id = t.wallet_id
		 WHERE to_char(t.transaction_date,'YYYY-MM')=$1
		   AND t.status='approved' AND t.deleted_at IS NULL%s`, flowSQL, flowSQL, walletFilter), period,
	).Scan(&income, &expense)

	var totalWallets float64
	conn.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(SUM(b.balance),0) FROM fin_wallet_balance b
		 JOIN fin_wallet w ON w.id=b.wallet_id
		 WHERE w.deleted_at IS NULL AND w.is_active=true%s`, walletFilter),
	).Scan(&totalWallets)

	var pendingCount int
	if isOwner(u) {
		conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM fin_transaction WHERE status='pending_approval' AND deleted_at IS NULL`,
		).Scan(&pendingCount)
	}

	// wallets snapshot
	walletVisibility := ""
	if !isOwner(u) {
		walletVisibility = " AND w.visibility='all'"
	}
	rows, _ := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT w.id, w.name, w.type, COALESCE(b.balance,w.initial_balance), w.currency, w.color, w.icon
		FROM fin_wallet w
		LEFT JOIN fin_wallet_balance b ON b.wallet_id=w.id
		WHERE w.deleted_at IS NULL AND w.is_active=true %s
		ORDER BY w.display_order, w.created_at LIMIT 8`, walletVisibility))
	var wallets []WalletSnap
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var ws WalletSnap
			var color, icon sql.NullString
			var bal float64
			rows.Scan(&ws.ID, &ws.Name, &ws.Type, &bal, &ws.Currency, &color, &icon)
			ws.Balance = moneyString(bal)
			if color.Valid {
				ws.Color = &color.String
			}
			if icon.Valid {
				ws.Icon = &icon.String
			}
			wallets = append(wallets, ws)
		}
	}
	if wallets == nil {
		wallets = []WalletSnap{}
	}

	net := income - expense
	return &DashboardSummary{
		Period:       period,
		TotalIncome:  moneyString(income),
		TotalExpense: moneyString(expense),
		NetBalance:   moneyString(net),
		TotalWallets: moneyString(totalWallets),
		PendingCount: pendingCount,
		Wallets:      wallets,
	}, nil
}

// ============================================================
// AUDIT LOG
// ============================================================

type AuditLogResponse struct {
	Items []AuditEntry `json:"items"`
}

type AuditEntry struct {
	ID         string          `json:"id"`
	EntityType string          `json:"entityType"`
	EntityID   string          `json:"entityId"`
	Action     string          `json:"action"`
	ActorID    string          `json:"actorId"`
	ActorRole  string          `json:"actorRole"`
	BeforeData json.RawMessage `json:"beforeData,omitempty"`
	AfterData  json.RawMessage `json:"afterData,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
}

type AuditLogParams struct {
	EntityType string `query:"entityType"`
	EntityID   string `query:"entityId"`
	Limit      int    `query:"limit"`
}

//encore:api auth method=GET path=/api/v1/finance/audit-log
func GetAuditLog(ctx context.Context, p *AuditLogParams) (*AuditLogResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tenant.CloseTenantConn(conn)

	limit := p.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	conditions := []string{"1=1"}
	args := []any{}
	i := 1
	if p.EntityType != "" {
		conditions = append(conditions, fmt.Sprintf("entity_type=$%d", i))
		args = append(args, p.EntityType)
		i++
	}
	if p.EntityID != "" {
		conditions = append(conditions, fmt.Sprintf("entity_id=$%d", i))
		args = append(args, p.EntityID)
		i++
	}
	args = append(args, limit)
	q := fmt.Sprintf(`SELECT id,entity_type,entity_id,action,actor_id,actor_role,before_data,after_data,created_at
		FROM fin_audit_log WHERE %s ORDER BY created_at DESC LIMIT $%d`,
		strings.Join(conditions, " AND "), i)

	rows, err := conn.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var items []AuditEntry
	for rows.Next() {
		var a AuditEntry
		var before, after json.RawMessage
		rows.Scan(&a.ID, &a.EntityType, &a.EntityID, &a.Action,
			&a.ActorID, &a.ActorRole, &before, &after, &a.CreatedAt)
		a.BeforeData = before
		a.AfterData = after
		items = append(items, a)
	}
	if items == nil {
		items = []AuditEntry{}
	}
	// record the audit-log read itself
	auditFinance(ctx, conn, u, "audit_log", u.TenantID, "export_audit", nil, p)
	return &AuditLogResponse{Items: items}, nil
}

var _ = rlog.Info // suppress unused import if rlog not used elsewhere
var _ = errs.Error{}
