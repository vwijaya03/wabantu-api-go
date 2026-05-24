package finance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/usage"
)

// ============================================================
// INVESTMENT & ASSET TRACKING
// ============================================================

type Asset struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Ticker         *string   `json:"ticker,omitempty"`
	Type           string    `json:"type"`
	UnitName       string    `json:"unitName"`
	UnitMultiplier string    `json:"unitMultiplier"`
	PriceUnitName  string    `json:"priceUnitName"`
	WalletID       string    `json:"walletId"`
	Notes          *string   `json:"notes,omitempty"`
	IsActive       bool      `json:"isActive"`
	CreatedAt      time.Time `json:"createdAt"`
}

type AssetWithPortfolio struct {
	Asset
	QtyHeld       string  `json:"qtyHeld"`
	QtyHeldBase   string  `json:"qtyHeldBase"`
	AvgBuyPrice   string  `json:"avgBuyPrice"`
	TotalCost     string  `json:"totalCost"`
	LatestPrice   *string `json:"latestPrice,omitempty"`
	CurrentValue  *string `json:"currentValue,omitempty"`
	UnrealizedPnL *string `json:"unrealizedPnl,omitempty"`
	UnrealizedPct *string `json:"unrealizedPct,omitempty"`
	TotalDividend string  `json:"totalDividend"`
}

type AssetPrice struct {
	ID         string    `json:"id"`
	AssetID    string    `json:"assetId"`
	Price      string    `json:"price"`
	RecordedAt time.Time `json:"recordedAt"`
	Source     string    `json:"source"`
}

type AssetListResponse struct {
	Assets []AssetWithPortfolio `json:"assets"`
}

type AssetPriceListResponse struct {
	Items []AssetPrice `json:"items"`
}

type PortfolioSummary struct {
	TotalCost     string               `json:"totalCost"`
	CurrentValue  string               `json:"currentValue"`
	UnrealizedPnL string               `json:"unrealizedPnl"`
	UnrealizedPct string               `json:"unrealizedPct"`
	TotalDividend string               `json:"totalDividend"`
	Total         int                  `json:"total"`
	Page          int                  `json:"page"`
	PageSize      int                  `json:"pageSize"`
	Assets        []AssetWithPortfolio `json:"assets"`
}

type GetPortfolioParams struct {
	Search   string `query:"search"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type CreateAssetParams struct {
	Name           string   `json:"name"`
	Ticker         *string  `json:"ticker,omitempty"`
	Type           string   `json:"type"`
	UnitName       string   `json:"unitName"`
	UnitMultiplier *float64 `json:"unitMultiplier,omitempty"`
	PriceUnitName  *string  `json:"priceUnitName,omitempty"`
	WalletID       string   `json:"walletId"`
	Notes          *string  `json:"notes,omitempty"`
}

type UpdateAssetParams struct {
	Name           *string  `json:"name,omitempty"`
	Ticker         *string  `json:"ticker,omitempty"`
	Type           *string  `json:"type,omitempty"`
	UnitName       *string  `json:"unitName,omitempty"`
	UnitMultiplier *float64 `json:"unitMultiplier,omitempty"`
	PriceUnitName  *string  `json:"priceUnitName,omitempty"`
	WalletID       *string  `json:"walletId,omitempty"`
	Notes          *string  `json:"notes,omitempty"`
}

type UpdateAssetPriceParams struct {
	AssetID string  `json:"assetId"`
	Price   float64 `json:"price"`
}

type RecordInvestmentTradeParams struct {
	Side            string   `json:"side"`
	Quantity        float64  `json:"quantity"`
	PricePerUnit    float64  `json:"pricePerUnit"`
	Fee             float64  `json:"fee"`
	FeePercent      *float64 `json:"feePercent,omitempty"`
	TransactionDate string   `json:"transactionDate"`
	Description     *string  `json:"description,omitempty"`
}

type AssetTrade struct {
	ID              string  `json:"id"`
	Type            string  `json:"type"`
	Quantity        string  `json:"quantity"`
	PricePerUnit    string  `json:"pricePerUnit"`
	Fee             string  `json:"fee"`
	Amount          string  `json:"amount"`
	TransactionDate string  `json:"transactionDate"`
	Description     *string `json:"description,omitempty"`
	Status          string  `json:"status"`
}

type AssetTradesResponse struct {
	Items []AssetTrade `json:"items"`
}

type RecordAssetDividendParams struct {
	Amount          float64 `json:"amount"`
	TransactionDate string  `json:"transactionDate"`
	Description     *string `json:"description,omitempty"`
}

type RecordAssetDividendResponse struct {
	TransactionID string `json:"transactionId"`
	Amount        string `json:"amount"`
	Status        string `json:"status"`
}

type RecordInvestmentTradeResponse struct {
	TransactionID string `json:"transactionId"`
	QtyHeld       string `json:"qtyHeld"`
	Amount        string `json:"amount"`
	Status        string `json:"status"`
}

var validAssetTypes = map[string]bool{
	"stock": true, "crypto": true, "gold": true, "mutual_fund": true, "other": true,
}

func resolveUnitMultiplier(assetType, unitName string, stored float64) float64 {
	if stored > 0 {
		return stored
	}
	return defaultUnitMultiplier(assetType, unitName)
}

func resolvePriceUnitName(assetType, unitName, stored string) string {
	if strings.TrimSpace(stored) != "" {
		return strings.TrimSpace(stored)
	}
	return defaultPriceUnitName(assetType, unitName)
}

func resolveTradeFee(gross float64, fee float64, feePercent *float64) (float64, error) {
	if feePercent != nil && *feePercent > 0 {
		if *feePercent > 100 {
			return 0, appErrs.BadRequest("persentase biaya maksimal 100")
		}
		return gross * *feePercent / 100, nil
	}
	if fee < 0 {
		return 0, appErrs.BadRequest("biaya tidak boleh negatif")
	}
	return fee, nil
}

func loadAssetUnitConfig(ctx context.Context, conn *sql.Conn, assetID string) (mult float64, err error) {
	var assetType, unitName string
	var stored sql.NullFloat64
	err = conn.QueryRowContext(ctx,
		`SELECT type, unit_name, unit_multiplier FROM fin_asset WHERE id=$1`, assetID,
	).Scan(&assetType, &unitName, &stored)
	if err != nil {
		return 1, err
	}
	m := 0.0
	if stored.Valid {
		m = stored.Float64
	}
	return resolveUnitMultiplier(assetType, unitName, m), nil
}

func finAssetTableReady(ctx context.Context, conn *sql.Conn) bool {
	var name sql.NullString
	_ = conn.QueryRowContext(ctx, `SELECT to_regclass('fin_asset')::text`).Scan(&name)
	return name.Valid && strings.TrimSpace(name.String) != ""
}

// loadAssetMetrics: asset_qty = jumlah dalam unit_name (mis. lot); harga = per price_unit_name (mis. lembar).
func loadAssetMetrics(ctx context.Context, conn *sql.Conn, assetID string, unitMult float64) (qtyHeld, avgBuy, totalCost, dividend float64, latestPrice sql.NullFloat64) {
	if unitMult <= 0 {
		unitMult = 1
	}
	var buyQty, buyAmt, sellQty float64
	_ = conn.QueryRowContext(ctx, `
		SELECT
		  COALESCE(SUM(CASE WHEN type='investment_buy' THEN asset_qty ELSE 0 END),0),
		  COALESCE(SUM(CASE WHEN type='investment_buy' THEN amount ELSE 0 END),0),
		  COALESCE(SUM(CASE WHEN type='investment_sell' THEN asset_qty ELSE 0 END),0),
		  COALESCE(SUM(CASE WHEN type='dividend' THEN amount ELSE 0 END),0)
		FROM fin_transaction
		WHERE asset_id=$1 AND status='approved' AND deleted_at IS NULL`, assetID,
	).Scan(&buyQty, &buyAmt, &sellQty, &dividend)

	qtyHeld = buyQty - sellQty
	qtyBase := qtyHeld * unitMult
	avgBuy = 0
	if buyQty > 0 {
		avgBuy = buyAmt / (buyQty * unitMult)
	}
	totalCost = avgBuy * qtyBase

	_ = conn.QueryRowContext(ctx,
		`SELECT price FROM fin_asset_price WHERE asset_id=$1 ORDER BY recorded_at DESC LIMIT 1`, assetID,
	).Scan(&latestPrice)
	return qtyHeld, avgBuy, totalCost, dividend, latestPrice
}

type assetRow struct {
	aw     AssetWithPortfolio
	ticker sql.NullString
	notes  sql.NullString
	mult   float64
}

func enrichAssetPortfolio(ctx context.Context, conn *sql.Conn, r assetRow) AssetWithPortfolio {
	aw := r.aw
	if r.ticker.Valid {
		aw.Ticker = &r.ticker.String
	}
	if r.notes.Valid {
		aw.Notes = &r.notes.String
	}

	mult := resolveUnitMultiplier(aw.Type, aw.UnitName, r.mult)
	aw.UnitMultiplier = fmt.Sprintf("%.0f", mult)
	aw.PriceUnitName = resolvePriceUnitName(aw.Type, aw.UnitName, aw.PriceUnitName)

	qtyHeld, avgBuy, totalCost, dividend, latestPrice := loadAssetMetrics(ctx, conn, aw.ID, mult)
	qtyBase := qtyHeld * mult
	aw.QtyHeld = fmt.Sprintf("%.6f", qtyHeld)
	aw.QtyHeldBase = fmt.Sprintf("%.6f", qtyBase)
	aw.AvgBuyPrice = fmt.Sprintf("%.4f", avgBuy)
	aw.TotalCost = fmt.Sprintf("%.2f", totalCost)
	aw.TotalDividend = fmt.Sprintf("%.2f", dividend)

	if latestPrice.Valid {
		lp := fmt.Sprintf("%.4f", latestPrice.Float64)
		aw.LatestPrice = &lp
		if qtyHeld > 0 {
			curVal := latestPrice.Float64 * qtyBase
			pnl := curVal - totalCost
			pct := 0.0
			if totalCost > 0 {
				pct = pnl / totalCost * 100
			}
			cv := fmt.Sprintf("%.2f", curVal)
			up := fmt.Sprintf("%.2f", pnl)
			upct := fmt.Sprintf("%.2f", pct)
			aw.CurrentValue = &cv
			aw.UnrealizedPnL = &up
			aw.UnrealizedPct = &upct
		}
	}
	return aw
}

func aggregatePortfolioTotals(assets []AssetWithPortfolio) (totalCost, totalValue, totalDiv float64) {
	for _, a := range assets {
		var tc float64
		fmt.Sscanf(a.TotalCost, "%f", &tc)
		totalCost += tc
		if a.CurrentValue != nil {
			var cv float64
			fmt.Sscanf(*a.CurrentValue, "%f", &cv)
			totalValue += cv
		} else {
			totalValue += tc
		}
		var td float64
		fmt.Sscanf(a.TotalDividend, "%f", &td)
		totalDiv += td
	}
	return totalCost, totalValue, totalDiv
}

func queryActiveAssets(ctx context.Context, conn *sql.Conn, search string) ([]assetRow, error) {
	q := `
		SELECT a.id, a.name, a.ticker, a.type, a.unit_name, a.wallet_id, a.notes, a.is_active, a.created_at,
		       COALESCE(a.unit_multiplier, 1), COALESCE(a.price_unit_name, '')
		FROM fin_asset a WHERE a.is_active=true`
	args := []any{}
	if s := strings.TrimSpace(search); s != "" {
		q += ` AND (a.name ILIKE $1 OR COALESCE(a.ticker, '') ILIKE $1)`
		args = append(args, "%"+s+"%")
	}
	q += ` ORDER BY a.name`

	rows, err := conn.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pending []assetRow
	for rows.Next() {
		var r assetRow
		var priceUnit sql.NullString
		if err := rows.Scan(&r.aw.ID, &r.aw.Name, &r.ticker, &r.aw.Type, &r.aw.UnitName, &r.aw.WalletID, &r.notes, &r.aw.IsActive, &r.aw.CreatedAt, &r.mult, &priceUnit); err != nil {
			return nil, err
		}
		if priceUnit.Valid {
			r.aw.PriceUnitName = priceUnit.String
		}
		pending = append(pending, r)
	}
	return pending, rows.Err()
}

//encore:api auth method=GET path=/api/v1/finance/investments/portfolio
func GetPortfolio(ctx context.Context, p *GetPortfolioParams) (*PortfolioSummary, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	page := 1
	pageSize := 20
	search := ""
	if p != nil {
		if p.Page > 0 {
			page = p.Page
		}
		if p.PageSize > 0 && p.PageSize <= 100 {
			pageSize = p.PageSize
		}
		search = strings.TrimSpace(p.Search)
	}

	if !finAssetTableReady(ctx, conn) {
		return &PortfolioSummary{
			TotalCost: "0.00", CurrentValue: "0.00", UnrealizedPnL: "0.00",
			UnrealizedPct: "0.00", TotalDividend: "0.00",
			Total: 0, Page: page, PageSize: pageSize,
			Assets: []AssetWithPortfolio{},
		}, nil
	}

	pending, err := queryActiveAssets(ctx, conn, search)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	var enriched []AssetWithPortfolio
	for _, r := range pending {
		enriched = append(enriched, enrichAssetPortfolio(ctx, conn, r))
	}
	if enriched == nil {
		enriched = []AssetWithPortfolio{}
	}

	total := len(enriched)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	paged := enriched[start:end]
	if paged == nil {
		paged = []AssetWithPortfolio{}
	}

	totalCost, totalValue, totalDiv := aggregatePortfolioTotals(enriched)
	pnl := totalValue - totalCost
	pct := 0.0
	if totalCost > 0 {
		pct = pnl / totalCost * 100
	}

	return &PortfolioSummary{
		TotalCost:     fmt.Sprintf("%.2f", totalCost),
		CurrentValue:  fmt.Sprintf("%.2f", totalValue),
		UnrealizedPnL: fmt.Sprintf("%.2f", pnl),
		UnrealizedPct: fmt.Sprintf("%.2f", pct),
		TotalDividend: fmt.Sprintf("%.2f", totalDiv),
		Total:         total,
		Page:          page,
		PageSize:      pageSize,
		Assets:        paged,
	}, nil
}

//encore:api auth method=POST path=/api/v1/finance/investments/assets
func CreateAsset(ctx context.Context, p *CreateAssetParams) (*Asset, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, appErrs.BadRequest("nama aset tidak boleh kosong")
	}
	if !validAssetTypes[p.Type] {
		return nil, appErrs.BadRequest("tipe aset tidak valid")
	}
	if strings.TrimSpace(p.UnitName) == "" {
		p.UnitName = defaultUnitNameForType(p.Type)
	} else {
		p.UnitName = strings.TrimSpace(p.UnitName)
	}
	if strings.TrimSpace(p.WalletID) == "" {
		return nil, appErrs.BadRequest("dompet wajib dipilih")
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	if err := assertWalletAccessible(ctx, conn, u, p.WalletID); err != nil {
		return nil, err
	}

	mult := defaultUnitMultiplier(p.Type, p.UnitName)
	if p.UnitMultiplier != nil && *p.UnitMultiplier > 0 {
		mult = *p.UnitMultiplier
	}
	priceUnit := defaultPriceUnitName(p.Type, p.UnitName)
	if p.PriceUnitName != nil && strings.TrimSpace(*p.PriceUnitName) != "" {
		priceUnit = strings.TrimSpace(*p.PriceUnitName)
	}

	var id string
	err = conn.QueryRowContext(ctx,
		`INSERT INTO fin_asset (name,ticker,type,unit_name,unit_multiplier,price_unit_name,wallet_id,notes,created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		strings.TrimSpace(p.Name), p.Ticker, p.Type, p.UnitName, mult, priceUnit,
		p.WalletID, p.Notes, u.AccountID,
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditFinance(ctx, conn, u, "asset", id, "create", nil, p)
	return &Asset{
		ID: id, Name: p.Name, Ticker: p.Ticker, Type: p.Type,
		UnitName: p.UnitName, UnitMultiplier: fmt.Sprintf("%.0f", mult),
		PriceUnitName: priceUnit, WalletID: p.WalletID, Notes: p.Notes,
		IsActive: true, CreatedAt: time.Now(),
	}, nil
}

//encore:api auth method=PUT path=/api/v1/finance/investments/assets/:id
func UpdateAsset(ctx context.Context, id string, p *UpdateAssetParams) (*Asset, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, appErrs.BadRequest("aset tidak valid")
	}
	if p == nil {
		return nil, appErrs.BadRequest("tidak ada perubahan")
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var isActive bool
	if err := conn.QueryRowContext(ctx,
		`SELECT is_active FROM fin_asset WHERE id=$1`, id,
	).Scan(&isActive); err == sql.ErrNoRows {
		return nil, appErrs.NotFound("aset tidak ditemukan")
	} else if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if !isActive {
		return nil, appErrs.NotFound("aset tidak ditemukan")
	}

	if p.Type != nil && !validAssetTypes[*p.Type] {
		return nil, appErrs.BadRequest("tipe aset tidak valid")
	}
	if p.Name != nil && strings.TrimSpace(*p.Name) == "" {
		return nil, appErrs.BadRequest("nama aset tidak boleh kosong")
	}
	if p.UnitName != nil && strings.TrimSpace(*p.UnitName) == "" {
		return nil, appErrs.BadRequest("satuan tidak boleh kosong")
	}

	if p.WalletID != nil && strings.TrimSpace(*p.WalletID) != "" {
		var txnCount int
		_ = conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM fin_transaction WHERE asset_id=$1 AND deleted_at IS NULL`, id,
		).Scan(&txnCount)
		if txnCount > 0 {
			return nil, appErrs.BadRequest("dompet tidak dapat diubah karena aset sudah memiliki transaksi")
		}
		if err := assertWalletAccessible(ctx, conn, u, *p.WalletID); err != nil {
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
	if p.Name != nil {
		add("name", strings.TrimSpace(*p.Name))
	}
	if p.Ticker != nil {
		t := strings.TrimSpace(*p.Ticker)
		if t == "" {
			add("ticker", nil)
		} else {
			add("ticker", t)
		}
	}
	if p.Type != nil {
		add("type", *p.Type)
	}
	if p.UnitName != nil {
		add("unit_name", strings.TrimSpace(*p.UnitName))
	}
	if p.UnitMultiplier != nil {
		if *p.UnitMultiplier <= 0 {
			return nil, appErrs.BadRequest("pengali unit harus lebih dari 0")
		}
		add("unit_multiplier", *p.UnitMultiplier)
	}
	if p.PriceUnitName != nil {
		pu := strings.TrimSpace(*p.PriceUnitName)
		if pu == "" {
			add("price_unit_name", nil)
		} else {
			add("price_unit_name", pu)
		}
	}
	if p.WalletID != nil && strings.TrimSpace(*p.WalletID) != "" {
		add("wallet_id", strings.TrimSpace(*p.WalletID))
	}
	if p.Notes != nil {
		n := strings.TrimSpace(*p.Notes)
		if n == "" {
			add("notes", nil)
		} else {
			add("notes", n)
		}
	}
	if len(sets) == 1 {
		return nil, appErrs.BadRequest("tidak ada perubahan")
	}

	args = append(args, id)
	_, err = conn.ExecContext(ctx,
		fmt.Sprintf(`UPDATE fin_asset SET %s WHERE id=$%d AND is_active=true`, strings.Join(sets, ","), i),
		args...,
	)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditFinance(ctx, conn, u, "asset", id, "edit", nil, p)

	var a Asset
	var ticker, notes sql.NullString
	var mult float64
	var priceUnit sql.NullString
	err = conn.QueryRowContext(ctx,
		`SELECT id, name, ticker, type, unit_name, wallet_id, notes, is_active, created_at,
		        COALESCE(unit_multiplier, 1), price_unit_name
		 FROM fin_asset WHERE id=$1`, id,
	).Scan(&a.ID, &a.Name, &ticker, &a.Type, &a.UnitName, &a.WalletID, &notes, &a.IsActive, &a.CreatedAt, &mult, &priceUnit)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if ticker.Valid {
		a.Ticker = &ticker.String
	}
	if notes.Valid {
		a.Notes = &notes.String
	}
	a.UnitMultiplier = fmt.Sprintf("%.0f", resolveUnitMultiplier(a.Type, a.UnitName, mult))
	a.PriceUnitName = resolvePriceUnitName(a.Type, a.UnitName, "")
	if priceUnit.Valid {
		a.PriceUnitName = resolvePriceUnitName(a.Type, a.UnitName, priceUnit.String)
	}
	return &a, nil
}

//encore:api auth method=DELETE path=/api/v1/finance/investments/assets/:id
func DeleteAsset(ctx context.Context, id string) (*OKResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, appErrs.BadRequest("aset tidak valid")
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var isActive bool
	if err := conn.QueryRowContext(ctx, `SELECT is_active FROM fin_asset WHERE id=$1`, id).Scan(&isActive); err == sql.ErrNoRows {
		return nil, appErrs.NotFound("aset tidak ditemukan")
	} else if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if !isActive {
		return &OKResponse{OK: true}, nil
	}

	mult, err := loadAssetUnitConfig(ctx, conn, id)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("aset tidak ditemukan")
	} else if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	qtyHeld, _, _, _, _ := loadAssetMetrics(ctx, conn, id, mult)
	if qtyHeld > 1e-6 {
		return nil, appErrs.BadRequest("aset masih memiliki kepemilikan. Catat penjualan terlebih dahulu sebelum menghapus.")
	}

	_, err = conn.ExecContext(ctx,
		`UPDATE fin_asset SET is_active=false, updated_at=now() WHERE id=$1`, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditFinance(ctx, conn, u, "asset", id, "delete", nil, nil)
	return &OKResponse{OK: true}, nil
}

//encore:api auth method=POST path=/api/v1/finance/investments/prices
func UpdateAssetPrice(ctx context.Context, p *UpdateAssetPriceParams) (*AssetPrice, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if p.Price <= 0 {
		return nil, appErrs.BadRequest("harga harus lebih dari 0")
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var id string
	err = conn.QueryRowContext(ctx,
		`INSERT INTO fin_asset_price (asset_id, price, recorded_by) VALUES ($1,$2,$3) RETURNING id`,
		p.AssetID, p.Price, u.AccountID,
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &AssetPrice{
		ID: id, AssetID: p.AssetID,
		Price:      fmt.Sprintf("%.4f", p.Price),
		RecordedAt: time.Now(), Source: "manual",
	}, nil
}

//encore:api auth method=POST path=/api/v1/finance/investments/assets/:id/trades
func RecordInvestmentTrade(ctx context.Context, id string, p *RecordInvestmentTradeParams) (*RecordInvestmentTradeResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, appErrs.BadRequest("aset tidak valid")
	}

	side := strings.ToLower(strings.TrimSpace(p.Side))
	if side != "buy" && side != "sell" {
		return nil, appErrs.BadRequest("side harus buy atau sell")
	}
	if p.Quantity <= 0 {
		return nil, appErrs.BadRequest("jumlah unit harus lebih dari 0")
	}
	if p.PricePerUnit <= 0 {
		return nil, appErrs.BadRequest("harga per unit harus lebih dari 0")
	}
	if p.TransactionDate == "" {
		p.TransactionDate = time.Now().Format("2006-01-02")
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var walletID, assetName string
	var isActive bool
	err = conn.QueryRowContext(ctx,
		`SELECT wallet_id, name, is_active FROM fin_asset WHERE id=$1`, id,
	).Scan(&walletID, &assetName, &isActive)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("aset tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if !isActive {
		return nil, appErrs.BadRequest("aset tidak aktif")
	}
	if err := assertWalletAccessible(ctx, conn, u, walletID); err != nil {
		return nil, err
	}

	txnType := "investment_buy"
	if side == "sell" {
		txnType = "investment_sell"
	}
	if _, err := loadTransactionTypeByCode(ctx, conn, txnType); err != nil {
		return nil, err
	}

	unitMult, err := loadAssetUnitConfig(ctx, conn, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	qtyHeld, _, _, _, _ := loadAssetMetrics(ctx, conn, id, unitMult)
	if side == "sell" && p.Quantity > qtyHeld+1e-6 {
		return nil, appErrs.BadRequest("jumlah jual melebihi kepemilikan")
	}

	// quantity = lot; price = per lembar (atau price_unit_name)
	gross := p.Quantity * unitMult * p.PricePerUnit
	resolvedFee, err := resolveTradeFee(gross, p.Fee, p.FeePercent)
	if err != nil {
		return nil, err
	}
	var amount float64
	if side == "buy" {
		amount = gross + resolvedFee
	} else {
		amount = gross - resolvedFee
		if amount <= 0 {
			return nil, appErrs.BadRequest("hasil jual harus lebih dari 0 setelah biaya")
		}
	}

	period := walletPeriod(p.TransactionDate)
	if err := ensurePeriodUnlocked(ctx, conn, period); err != nil {
		return nil, err
	}

	status := "approved"
	if !isOwner(u) {
		cfg, err := loadApprovalConfig(ctx, conn)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if staffNeedsApproval(cfg, txnType, amount) {
			status = "pending_approval"
		}
	}

	desc := p.Description
	if desc == nil || strings.TrimSpace(*desc) == "" {
		label := "Beli"
		if side == "sell" {
			label = "Jual"
		}
		d := fmt.Sprintf("%s %s", label, assetName)
		desc = &d
	}

	var txnID string
	err = conn.QueryRowContext(ctx,
		`INSERT INTO fin_transaction
		 (type,amount,currency,wallet_id,description,transaction_date,status,
		  asset_id,asset_qty,asset_price_per_unit,asset_fee,created_by)
		 VALUES ($1,$2,'IDR',$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 RETURNING id`,
		txnType, amount, walletID, desc, p.TransactionDate, status,
		id, p.Quantity, p.PricePerUnit, resolvedFee, u.AccountID,
	).Scan(&txnID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	if status == "approved" {
		refreshWallets(ctx, conn, walletID, nil)
	}
	usage.RecordEvent(ctx, u.TenantSchema, "fin_transaction_created", 1, nil)
	auditFinance(ctx, conn, u, "transaction", txnID, "create", nil, p)

	newQty, _, _, _, _ := loadAssetMetrics(ctx, conn, id, unitMult)
	return &RecordInvestmentTradeResponse{
		TransactionID: txnID,
		QtyHeld:       fmt.Sprintf("%.6f", newQty),
		Amount:        fmt.Sprintf("%.2f", amount),
		Status:        status,
	}, nil
}

//encore:api auth method=POST path=/api/v1/finance/investments/assets/:id/dividends
func RecordAssetDividend(ctx context.Context, id string, p *RecordAssetDividendParams) (*RecordAssetDividendResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, appErrs.BadRequest("aset tidak valid")
	}
	if p.Amount <= 0 {
		return nil, appErrs.BadRequest("jumlah dividen harus lebih dari 0")
	}
	if p.TransactionDate == "" {
		p.TransactionDate = time.Now().Format("2006-01-02")
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var walletID, assetName string
	var isActive bool
	err = conn.QueryRowContext(ctx,
		`SELECT wallet_id, name, is_active FROM fin_asset WHERE id=$1`, id,
	).Scan(&walletID, &assetName, &isActive)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("aset tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if !isActive {
		return nil, appErrs.BadRequest("aset tidak aktif")
	}
	if err := assertWalletAccessible(ctx, conn, u, walletID); err != nil {
		return nil, err
	}
	if _, err := loadTransactionTypeByCode(ctx, conn, "dividend"); err != nil {
		return nil, err
	}

	period := walletPeriod(p.TransactionDate)
	if err := ensurePeriodUnlocked(ctx, conn, period); err != nil {
		return nil, err
	}

	status := "approved"
	if !isOwner(u) {
		cfg, err := loadApprovalConfig(ctx, conn)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if staffNeedsApproval(cfg, "dividend", p.Amount) {
			status = "pending_approval"
		}
	}

	desc := p.Description
	if desc == nil || strings.TrimSpace(*desc) == "" {
		d := fmt.Sprintf("Dividen %s", assetName)
		desc = &d
	}

	var txnID string
	err = conn.QueryRowContext(ctx,
		`INSERT INTO fin_transaction
		 (type,amount,currency,wallet_id,description,transaction_date,status,asset_id,created_by)
		 VALUES ('dividend',$1,'IDR',$2,$3,$4,$5,$6,$7)
		 RETURNING id`,
		p.Amount, walletID, desc, p.TransactionDate, status, id, u.AccountID,
	).Scan(&txnID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	if status == "approved" {
		refreshWallets(ctx, conn, walletID, nil)
	}
	usage.RecordEvent(ctx, u.TenantSchema, "fin_transaction_created", 1, nil)
	auditFinance(ctx, conn, u, "transaction", txnID, "create", nil, p)

	return &RecordAssetDividendResponse{
		TransactionID: txnID,
		Amount:        fmt.Sprintf("%.2f", p.Amount),
		Status:        status,
	}, nil
}

//encore:api auth method=GET path=/api/v1/finance/investments/assets/:id/trades
func ListAssetTrades(ctx context.Context, id string) (*AssetTradesResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, appErrs.BadRequest("aset tidak valid")
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx, `
		SELECT id, type, COALESCE(asset_qty,0), COALESCE(asset_price_per_unit,0), COALESCE(asset_fee,0),
		       amount, transaction_date::text, description, status
		FROM fin_transaction
		WHERE asset_id=$1 AND deleted_at IS NULL
		  AND type IN ('investment_buy','investment_sell','dividend')
		ORDER BY transaction_date DESC, created_at DESC`, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var items []AssetTrade
	for rows.Next() {
		var t AssetTrade
		var qty, price, fee, amt float64
		var desc sql.NullString
		if err := rows.Scan(&t.ID, &t.Type, &qty, &price, &fee, &amt, &t.TransactionDate, &desc, &t.Status); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		t.Quantity = fmt.Sprintf("%.6f", qty)
		t.PricePerUnit = fmt.Sprintf("%.4f", price)
		t.Fee = fmt.Sprintf("%.2f", fee)
		t.Amount = fmt.Sprintf("%.2f", amt)
		if desc.Valid {
			t.Description = &desc.String
		}
		items = append(items, t)
	}
	if items == nil {
		items = []AssetTrade{}
	}
	return &AssetTradesResponse{Items: items}, nil
}

//encore:api auth method=DELETE path=/api/v1/finance/investments/assets/:id/trades/:txnId
func DeleteAssetTrade(ctx context.Context, id string, txnId string) (*OKResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(txnId) == "" {
		return nil, appErrs.BadRequest("parameter tidak valid")
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var walletID, txType, txDate string
	var toWalletID sql.NullString
	err = conn.QueryRowContext(ctx, `
		SELECT wallet_id, to_wallet_id, type, transaction_date::text
		FROM fin_transaction
		WHERE id=$1 AND asset_id=$2 AND deleted_at IS NULL`, txnId, id,
	).Scan(&walletID, &toWalletID, &txType, &txDate)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("transaksi tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if txType != "investment_buy" && txType != "investment_sell" && txType != "dividend" {
		return nil, appErrs.BadRequest("hanya transaksi investasi yang dapat dihapus dari sini")
	}

	if err := ensurePeriodUnlocked(ctx, conn, walletPeriod(txDate)); err != nil {
		return nil, err
	}

	_, err = conn.ExecContext(ctx,
		`UPDATE fin_transaction SET deleted_at=now(), deleted_by=$1 WHERE id=$2`, u.AccountID, txnId)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	var toPtr *string
	if toWalletID.Valid && toWalletID.String != "" {
		toPtr = &toWalletID.String
	}
	refreshWallets(ctx, conn, walletID, toPtr)
	auditFinance(ctx, conn, u, "transaction", txnId, "delete", nil, nil)
	return &OKResponse{OK: true}, nil
}

//encore:api auth method=GET path=/api/v1/finance/investments/assets/:id/prices
func GetAssetPriceHistory(ctx context.Context, id string) (*AssetPriceListResponse, error) {
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
	defer conn.Close()

	rows, err := conn.QueryContext(ctx,
		`SELECT id, asset_id, price, recorded_at, source
		 FROM fin_asset_price WHERE asset_id=$1 ORDER BY recorded_at DESC LIMIT 30`, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var items []AssetPrice
	for rows.Next() {
		var ap AssetPrice
		var price float64
		rows.Scan(&ap.ID, &ap.AssetID, &price, &ap.RecordedAt, &ap.Source)
		ap.Price = fmt.Sprintf("%.4f", price)
		items = append(items, ap)
	}
	if items == nil {
		items = []AssetPrice{}
	}
	return &AssetPriceListResponse{Items: items}, nil
}

// suppress unused
var _ = sql.ErrNoRows
