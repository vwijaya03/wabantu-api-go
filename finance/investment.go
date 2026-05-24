package finance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
)

// ============================================================
// INVESTMENT & ASSET TRACKING
// ============================================================

type Asset struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Ticker    *string   `json:"ticker,omitempty"`
	Type      string    `json:"type"`
	UnitName  string    `json:"unitName"`
	WalletID  string    `json:"walletId"`
	Notes     *string   `json:"notes,omitempty"`
	IsActive  bool      `json:"isActive"`
	CreatedAt time.Time `json:"createdAt"`
}

type AssetWithPortfolio struct {
	Asset
	QtyHeld        string  `json:"qtyHeld"`
	AvgBuyPrice    string  `json:"avgBuyPrice"`
	TotalCost      string  `json:"totalCost"`
	LatestPrice    *string `json:"latestPrice,omitempty"`
	CurrentValue   *string `json:"currentValue,omitempty"`
	UnrealizedPnL  *string `json:"unrealizedPnl,omitempty"`
	UnrealizedPct  *string `json:"unrealizedPct,omitempty"`
	TotalDividend  string  `json:"totalDividend"`
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
	TotalCost      string               `json:"totalCost"`
	CurrentValue   string               `json:"currentValue"`
	UnrealizedPnL  string               `json:"unrealizedPnl"`
	UnrealizedPct  string               `json:"unrealizedPct"`
	TotalDividend  string               `json:"totalDividend"`
	Assets         []AssetWithPortfolio `json:"assets"`
}

type CreateAssetParams struct {
	Name     string  `json:"name"`
	Ticker   *string `json:"ticker,omitempty"`
	Type     string  `json:"type"`
	UnitName string  `json:"unitName"`
	WalletID string  `json:"walletId"`
	Notes    *string `json:"notes,omitempty"`
}

type UpdateAssetPriceParams struct {
	AssetID string  `json:"assetId"`
	Price   float64 `json:"price"`
}

var validAssetTypes = map[string]bool{
	"stock": true, "crypto": true, "gold": true, "mutual_fund": true, "other": true,
}

//encore:api auth method=GET path=/api/v1/finance/investments/portfolio
func GetPortfolio(ctx context.Context) (*PortfolioSummary, error) {
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

	rows, err := conn.QueryContext(ctx, `
		SELECT a.id, a.name, a.ticker, a.type, a.unit_name, a.wallet_id, a.notes, a.is_active, a.created_at
		FROM fin_asset a WHERE a.is_active=true ORDER BY a.name`)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var assets []AssetWithPortfolio
	for rows.Next() {
		var aw AssetWithPortfolio
		var ticker, notes sql.NullString
		rows.Scan(&aw.ID, &aw.Name, &ticker, &aw.Type, &aw.UnitName, &aw.WalletID, &notes, &aw.IsActive, &aw.CreatedAt)
		if ticker.Valid {
			aw.Ticker = &ticker.String
		}
		if notes.Valid {
			aw.Notes = &notes.String
		}
		// Compute qty, avg price, total cost from transactions
		var buyQty, buyAmt, sellQty, dividend float64
		conn.QueryRowContext(ctx, `
			SELECT
			  COALESCE(SUM(CASE WHEN type='investment_buy' THEN asset_qty ELSE 0 END),0),
			  COALESCE(SUM(CASE WHEN type='investment_buy' THEN amount ELSE 0 END),0),
			  COALESCE(SUM(CASE WHEN type='investment_sell' THEN asset_qty ELSE 0 END),0),
			  COALESCE(SUM(CASE WHEN type='dividend' THEN amount ELSE 0 END),0)
			FROM fin_transaction
			WHERE asset_id=$1 AND status='approved' AND deleted_at IS NULL`, aw.ID,
		).Scan(&buyQty, &buyAmt, &sellQty, &dividend)

		qtyHeld := buyQty - sellQty
		avgBuy := 0.0
		if buyQty > 0 {
			avgBuy = buyAmt / buyQty
		}
		totalCost := avgBuy * qtyHeld
		aw.QtyHeld = fmt.Sprintf("%.6f", qtyHeld)
		aw.AvgBuyPrice = fmt.Sprintf("%.4f", avgBuy)
		aw.TotalCost = fmt.Sprintf("%.2f", totalCost)
		aw.TotalDividend = fmt.Sprintf("%.2f", dividend)

		// Latest price
		var latestPrice sql.NullFloat64
		conn.QueryRowContext(ctx,
			`SELECT price FROM fin_asset_price WHERE asset_id=$1 ORDER BY recorded_at DESC LIMIT 1`, aw.ID,
		).Scan(&latestPrice)
		if latestPrice.Valid && qtyHeld > 0 {
			curVal := latestPrice.Float64 * qtyHeld
			pnl := curVal - totalCost
			pct := 0.0
			if totalCost > 0 {
				pct = pnl / totalCost * 100
			}
			lp := fmt.Sprintf("%.4f", latestPrice.Float64)
			cv := fmt.Sprintf("%.2f", curVal)
			up := fmt.Sprintf("%.2f", pnl)
			upct := fmt.Sprintf("%.2f", pct)
			aw.LatestPrice = &lp
			aw.CurrentValue = &cv
			aw.UnrealizedPnL = &up
			aw.UnrealizedPct = &upct
		}
		assets = append(assets, aw)
	}
	if assets == nil {
		assets = []AssetWithPortfolio{}
	}

	// Aggregate totals
	var totalCost, totalValue, totalDiv float64
	for _, a := range assets {
		fmt.Sscanf(a.TotalCost, "%f", &totalCost)
		if a.CurrentValue != nil {
			var cv float64
			fmt.Sscanf(*a.CurrentValue, "%f", &cv)
			totalValue += cv
		} else {
			var tc float64
			fmt.Sscanf(a.TotalCost, "%f", &tc)
			totalValue += tc
		}
		var td float64
		fmt.Sscanf(a.TotalDividend, "%f", &td)
		totalDiv += td
	}
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
		Assets:        assets,
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
	if p.UnitName == "" {
		p.UnitName = "lot"
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var id string
	err = conn.QueryRowContext(ctx,
		`INSERT INTO fin_asset (name,ticker,type,unit_name,wallet_id,notes,created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		strings.TrimSpace(p.Name), p.Ticker, p.Type, p.UnitName, p.WalletID, p.Notes, u.AccountID,
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditFinance(ctx, conn, u, "asset", id, "create", nil, p)
	return &Asset{
		ID: id, Name: p.Name, Ticker: p.Ticker, Type: p.Type,
		UnitName: p.UnitName, WalletID: p.WalletID, Notes: p.Notes,
		IsActive: true, CreatedAt: time.Now(),
	}, nil
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
