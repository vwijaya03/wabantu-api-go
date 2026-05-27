package business

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/pricing"
)

type CatalogItemPrice struct {
	PriceTypeID    string  `json:"priceTypeId"`
	PriceTypeCode  string  `json:"priceTypeCode,omitempty"`
	PriceTypeLabel string  `json:"priceTypeLabel,omitempty"`
	Price          float64 `json:"price"`
}

// EnsurePricingSchema applies idempotent DDL for price types and catalog prices.
func EnsurePricingSchema(ctx context.Context, conn *sql.Conn) error {
	return ensurePricingSchema(ctx, conn)
}

func ensurePricingSchema(ctx context.Context, conn *sql.Conn) error {
	if err := pricing.EnsureSchema(ctx, conn); err != nil {
		return err
	}
	return ensureCatalogIndexes(ctx, conn)
}

// ensureCatalogIndexes fixes legacy unique index that blocked re-create after soft delete.
func ensureCatalogIndexes(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, `
		DROP INDEX IF EXISTS idx_catalog_source_code;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_catalog_source_code
			ON business_catalog_item(source, external_code)
			WHERE deleted_at IS NULL;
	`)
	return err
}

func attachCatalogItemPricesBatch(ctx context.Context, conn *sql.Conn, items []CatalogItem) error {
	for i := range items {
		prices, err := loadCatalogItemPrices(ctx, conn, items[i].ID)
		if err != nil {
			return err
		}
		items[i].Prices = prices
	}
	return nil
}

func seedDefaultPriceTypes(ctx context.Context, conn *sql.Conn) error {
	type row struct {
		code, label string
		order       int
		isDefault   bool
		isSystem    bool
	}
	rows := []row{
		{"umum", "Harga umum", 1, true, true},
		{"reseller", "Harga reseller", 2, false, true},
	}
	for _, r := range rows {
		var exists bool
		if err := conn.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM business_price_type WHERE code=$1 AND deleted_at IS NULL)`, r.code,
		).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO business_price_type (code, label, display_order, is_default, is_system, is_active)
			VALUES ($1,$2,$3,$4,$5,true)`,
			r.code, r.label, r.order, r.isDefault, r.isSystem,
		); err != nil {
			return err
		}
	}

	// Backfill umum prices from legacy sell_price column.
	_, err := conn.ExecContext(ctx, `
		INSERT INTO business_catalog_item_price (catalog_item_id, price_type_id, price)
		SELECT c.id, pt.id, c.sell_price
		FROM business_catalog_item c
		JOIN business_price_type pt ON pt.code = 'umum' AND pt.deleted_at IS NULL
		WHERE c.deleted_at IS NULL AND c.sell_price IS NOT NULL
		ON CONFLICT (catalog_item_id, price_type_id) DO NOTHING`)
	return err
}

func resolveDefaultPriceTypeID(ctx context.Context, conn *sql.Conn) (string, error) {
	return pricing.ResolveDefaultPriceTypeID(ctx, conn)
}

func resolvePriceTypeIDForContact(ctx context.Context, conn *sql.Conn, contactID string) (string, error) {
	return pricing.ResolvePriceTypeIDForContact(ctx, conn, contactID)
}

func resolveCatalogUnitPrice(ctx context.Context, conn *sql.Conn, catalogItemID, priceTypeID string) (float64, error) {
	return pricing.ResolveCatalogUnitPrice(ctx, conn, catalogItemID, priceTypeID)
}

func loadCatalogItemPrices(ctx context.Context, conn *sql.Conn, catalogItemID string) ([]CatalogItemPrice, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT pt.id::text, pt.code, pt.label, p.price
		FROM business_catalog_item_price p
		JOIN business_price_type pt
		  ON pt.id = p.price_type_id AND pt.deleted_at IS NULL AND pt.is_active = true
		WHERE p.catalog_item_id = $1::uuid
		ORDER BY pt.display_order, pt.label`, catalogItemID)
	if err != nil {
		return nil, apperr.Internal("list catalog prices failed")
	}
	defer rows.Close()

	out := make([]CatalogItemPrice, 0)
	for rows.Next() {
		var row CatalogItemPrice
		if err := rows.Scan(&row.PriceTypeID, &row.PriceTypeCode, &row.PriceTypeLabel, &row.Price); err != nil {
			return nil, apperr.Internal("scan catalog price failed")
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func upsertCatalogItemPrices(ctx context.Context, conn *sql.Conn, catalogItemID string, prices []CatalogItemPrice) error {
	for _, p := range prices {
		if strings.TrimSpace(p.PriceTypeID) == "" {
			continue
		}
		if p.Price < 0 {
			return apperr.BadRequest(fmt.Sprintf("harga tidak boleh negatif untuk tipe %s", p.PriceTypeLabel))
		}
		_, err := conn.ExecContext(ctx, `
			INSERT INTO business_catalog_item_price (catalog_item_id, price_type_id, price)
			VALUES ($1::uuid, $2::uuid, $3)
			ON CONFLICT (catalog_item_id, price_type_id)
			DO UPDATE SET price = EXCLUDED.price, updated_at = now()`,
			catalogItemID, p.PriceTypeID, p.Price,
		)
		if err != nil {
			return apperr.Internal("upsert catalog price failed")
		}

		// Keep sell_price in sync with default (umum) price type.
		var isDefault bool
		_ = conn.QueryRowContext(ctx,
			`SELECT is_default FROM business_price_type WHERE id = $1::uuid AND deleted_at IS NULL`,
			p.PriceTypeID,
		).Scan(&isDefault)
		if isDefault {
			_, _ = conn.ExecContext(ctx, `
				UPDATE business_catalog_item SET sell_price = $1, updated_at = now()
				WHERE id = $2::uuid AND deleted_at IS NULL`, p.Price, catalogItemID)
		}
	}
	return nil
}

func attachEffectiveSellPrices(ctx context.Context, conn *sql.Conn, items []CatalogItem, contactID string) error {
	priceTypeID, err := resolvePriceTypeIDForContact(ctx, conn, contactID)
	if err != nil {
		return err
	}
	for i := range items {
		price, err := resolveCatalogUnitPrice(ctx, conn, items[i].ID, priceTypeID)
		if err != nil {
			return err
		}
		items[i].EffectiveSellPrice = &price
	}
	return nil
}
