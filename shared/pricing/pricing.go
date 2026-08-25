package pricing

import (
	"context"
	"database/sql"
	"strings"

	"encore.app/wabantu/tenant"
	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/tenantschema"
	"encore.dev"
)

// EnsureSchema applies idempotent DDL for price types and catalog prices.
// On Encore Cloud the app DB role cannot run DDL; skip when schema is already present.
func EnsureSchema(ctx context.Context, conn *sql.Conn, tenantSchema string) error {
	ready, err := tenantschema.PricingReady(ctx, conn)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return tenant.EnsureCloudAdminTenantDDL(ctx, tenantSchema)
	}
	_, err = conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS business_price_type (
			id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			code           VARCHAR(40)  NOT NULL,
			label          VARCHAR(100) NOT NULL,
			display_order  INT          NOT NULL DEFAULT 0,
			is_default     BOOLEAN      NOT NULL DEFAULT false,
			is_system      BOOLEAN      NOT NULL DEFAULT false,
			is_active      BOOLEAN      NOT NULL DEFAULT true,
			created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
			updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
			deleted_at     TIMESTAMPTZ
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_price_type_code
			ON business_price_type(code) WHERE deleted_at IS NULL;

		CREATE TABLE IF NOT EXISTS business_catalog_item_price (
			catalog_item_id UUID          NOT NULL,
			price_type_id   UUID          NOT NULL,
			price           NUMERIC(15,4) NOT NULL CHECK (price >= 0),
			updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
			PRIMARY KEY (catalog_item_id, price_type_id)
		);
		CREATE INDEX IF NOT EXISTS idx_catalog_item_price_type
			ON business_catalog_item_price(price_type_id);

		ALTER TABLE contact ADD COLUMN IF NOT EXISTS price_type_id UUID;
	`)
	return err
}

// ResolveDefaultPriceTypeID returns the tenant default active price type.
func ResolveDefaultPriceTypeID(ctx context.Context, conn *sql.Conn) (string, error) {
	var id string
	err := conn.QueryRowContext(ctx, `
		SELECT id::text FROM business_price_type
		WHERE deleted_at IS NULL AND is_active = true
		ORDER BY is_default DESC, display_order, label
		LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return "", apperr.BadRequest("belum ada tipe harga aktif")
	}
	if err != nil {
		return "", apperr.Internal("resolve default price type failed")
	}
	return id, nil
}

// ResolvePriceTypeIDForContact returns the price type for a contact, or tenant default.
func ResolvePriceTypeIDForContact(ctx context.Context, conn *sql.Conn, contactID string) (string, error) {
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return ResolveDefaultPriceTypeID(ctx, conn)
	}
	var priceTypeID sql.NullString
	err := conn.QueryRowContext(ctx, `
		SELECT price_type_id::text FROM contact
		WHERE id = $1::uuid AND deleted_at IS NULL`, contactID,
	).Scan(&priceTypeID)
	if err == sql.ErrNoRows {
		return ResolveDefaultPriceTypeID(ctx, conn)
	}
	if err != nil {
		return "", apperr.Internal("load contact price type failed")
	}
	if priceTypeID.Valid && strings.TrimSpace(priceTypeID.String) != "" {
		var active bool
		if err := conn.QueryRowContext(ctx, `
			SELECT is_active FROM business_price_type
			WHERE id = $1::uuid AND deleted_at IS NULL`,
			priceTypeID.String,
		).Scan(&active); err == nil && active {
			return priceTypeID.String, nil
		}
	}
	return ResolveDefaultPriceTypeID(ctx, conn)
}

// ResolveCatalogUnitPrice returns unit price for a catalog row and price type.
func ResolveCatalogUnitPrice(ctx context.Context, conn *sql.Conn, catalogItemID, priceTypeID string) (float64, error) {
	var price sql.NullFloat64
	err := conn.QueryRowContext(ctx, `
		SELECT p.price
		FROM business_catalog_item_price p
		WHERE p.catalog_item_id = $1::uuid AND p.price_type_id = $2::uuid`,
		catalogItemID, priceTypeID,
	).Scan(&price)
	if err == nil && price.Valid {
		return price.Float64, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return 0, apperr.Internal("load catalog price failed")
	}

	err = conn.QueryRowContext(ctx, `
		SELECT sell_price FROM business_catalog_item
		WHERE id = $1::uuid AND deleted_at IS NULL`, catalogItemID,
	).Scan(&price)
	if err == sql.ErrNoRows {
		return 0, apperr.NotFound("catalog item not found")
	}
	if err != nil {
		return 0, apperr.Internal("load catalog sell price failed")
	}
	if price.Valid {
		return price.Float64, nil
	}
	return 0, nil
}
