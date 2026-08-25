package business

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	appdb "encore.app/wabantu/shared/db"
	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/pricing"
	"encore.app/wabantu/shared/tenantschema"
	"encore.app/wabantu/tenant"
	"encore.dev"
)

type CatalogItemPrice struct {
	PriceTypeID    string  `json:"priceTypeId"`
	PriceTypeCode  string  `json:"priceTypeCode,omitempty"`
	PriceTypeLabel string  `json:"priceTypeLabel,omitempty"`
	Price          float64 `json:"price"`
}

// EnsurePricingSchema applies idempotent DDL for price types and catalog prices.
func EnsurePricingSchema(ctx context.Context, tenantSchema string) error {
	return ensurePricingSchema(ctx, tenantSchema)
}

func ensurePricingSchema(ctx context.Context, tenantSchema string) error {
	pool := tenantDB.Stdlib()
	if err := pricing.EnsureSchema(ctx, pool, tenantSchema); err != nil {
		return err
	}
	return ensureCatalogIndexes(ctx, pool, tenantSchema)
}

// ensureCatalogIndexes fixes legacy unique index that blocked re-create after soft delete.
func ensureCatalogIndexes(ctx context.Context, q any, tenantSchema string) error {
	exists, err := tenantschema.CatalogIndexReady(ctx, q, tenantSchema)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		return tenant.EnsureCloudAdminTenantDDL(ctx, tenantSchema)
	}
	sch := appdb.SchemaSQL{Schema: tenantSchema}
	catalogItem := sch.T("business_catalog_item")
	querier := tenantschema.Q(q)
	_, err = querier.ExecContext(ctx, fmt.Sprintf(`
		DROP INDEX IF EXISTS idx_catalog_source_code;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_catalog_source_code
			ON %s(source, external_code)
			WHERE deleted_at IS NULL;
	`, catalogItem))
	return err
}

func attachCatalogItemPricesBatch(ctx context.Context, ts appdb.TenantScope, items []CatalogItem) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, len(items))
	idIndex := make(map[string][]int, len(items))
	for i := range items {
		ids[i] = items[i].ID
		idIndex[items[i].ID] = append(idIndex[items[i].ID], i)
	}
	batch, err := loadCatalogItemPricesBatch(ctx, ts, ids)
	if err != nil {
		return err
	}
	for id, prices := range batch {
		for _, idx := range idIndex[id] {
			items[idx].Prices = prices
		}
	}
	return nil
}

func loadCatalogItemPricesBatch(ctx context.Context, ts appdb.TenantScope, ids []string) (map[string][]CatalogItemPrice, error) {
	inClause, args := uuidInClause(ids)
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT p.catalog_item_id::text, pt.id::text, pt.code, pt.label, p.price
		FROM business_catalog_item_price p
		JOIN business_price_type pt
		  ON pt.id = p.price_type_id AND pt.deleted_at IS NULL AND pt.is_active = true
		WHERE p.catalog_item_id IN (%s)
		ORDER BY p.catalog_item_id, pt.display_order, pt.label`, inClause), args...)
	if err != nil {
		return nil, apperr.Internal("list catalog prices failed")
	}
	defer rows.Close()
	out := make(map[string][]CatalogItemPrice)
	for rows.Next() {
		var itemID string
		var row CatalogItemPrice
		if err := rows.Scan(&itemID, &row.PriceTypeID, &row.PriceTypeCode, &row.PriceTypeLabel, &row.Price); err != nil {
			return nil, apperr.Internal("scan catalog price failed")
		}
		out[itemID] = append(out[itemID], row)
	}
	return out, rows.Err()
}

func seedDefaultPriceTypes(ctx context.Context, ts appdb.TenantScope) error {
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
		if err := ts.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM business_price_type WHERE code=$1 AND deleted_at IS NULL)`, r.code,
		).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := ts.ExecContext(ctx, `
			INSERT INTO business_price_type (code, label, display_order, is_default, is_system, is_active)
			VALUES ($1,$2,$3,$4,$5,true)`,
			r.code, r.label, r.order, r.isDefault, r.isSystem,
		); err != nil {
			return err
		}
	}

	_, err := ts.ExecContext(ctx, `
		INSERT INTO business_catalog_item_price (catalog_item_id, price_type_id, price)
		SELECT c.id, pt.id, c.sell_price
		FROM business_catalog_item c
		JOIN business_price_type pt ON pt.code = 'umum' AND pt.deleted_at IS NULL
		WHERE c.deleted_at IS NULL AND c.sell_price IS NOT NULL
		ON CONFLICT (catalog_item_id, price_type_id) DO NOTHING`)
	return err
}

func resolveDefaultPriceTypeID(ctx context.Context, ts appdb.TenantScope) (string, error) {
	var id string
	err := ts.QueryRowContext(ctx, `
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

func resolvePriceTypeIDForContact(ctx context.Context, ts appdb.TenantScope, contactID string) (string, error) {
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return resolveDefaultPriceTypeID(ctx, ts)
	}
	var priceTypeID sql.NullString
	err := ts.QueryRowContext(ctx, `
		SELECT price_type_id::text FROM contact
		WHERE id = $1::uuid AND deleted_at IS NULL`, contactID,
	).Scan(&priceTypeID)
	if err == sql.ErrNoRows {
		return resolveDefaultPriceTypeID(ctx, ts)
	}
	if err != nil {
		return "", apperr.Internal("load contact price type failed")
	}
	if priceTypeID.Valid && strings.TrimSpace(priceTypeID.String) != "" {
		var active bool
		if err := ts.QueryRowContext(ctx, `
			SELECT is_active FROM business_price_type
			WHERE id = $1::uuid AND deleted_at IS NULL`,
			priceTypeID.String,
		).Scan(&active); err == nil && active {
			return priceTypeID.String, nil
		}
	}
	return resolveDefaultPriceTypeID(ctx, ts)
}

func resolveCatalogUnitPrice(ctx context.Context, ts appdb.TenantScope, catalogItemID, priceTypeID string) (float64, error) {
	var price sql.NullFloat64
	err := ts.QueryRowContext(ctx, `
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

	err = ts.QueryRowContext(ctx, `
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

func loadCatalogItemPrices(ctx context.Context, ts appdb.TenantScope, catalogItemID string) ([]CatalogItemPrice, error) {
	rows, err := ts.QueryContext(ctx, `
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

func upsertCatalogItemPrices(ctx context.Context, ts appdb.TenantScope, catalogItemID string, prices []CatalogItemPrice) error {
	for _, p := range prices {
		if strings.TrimSpace(p.PriceTypeID) == "" {
			continue
		}
		if p.Price < 0 {
			return apperr.BadRequest(fmt.Sprintf("harga tidak boleh negatif untuk tipe %s", p.PriceTypeLabel))
		}
		_, err := ts.ExecContext(ctx, `
			INSERT INTO business_catalog_item_price (catalog_item_id, price_type_id, price)
			VALUES ($1::uuid, $2::uuid, $3)
			ON CONFLICT (catalog_item_id, price_type_id)
			DO UPDATE SET price = EXCLUDED.price, updated_at = now()`,
			catalogItemID, p.PriceTypeID, p.Price,
		)
		if err != nil {
			return apperr.Internal("upsert catalog price failed")
		}

		var isDefault bool
		_ = ts.QueryRowContext(ctx,
			`SELECT is_default FROM business_price_type WHERE id = $1::uuid AND deleted_at IS NULL`,
			p.PriceTypeID,
		).Scan(&isDefault)
		if isDefault {
			_, _ = ts.ExecContext(ctx, `
				UPDATE business_catalog_item SET sell_price = $1, updated_at = now()
				WHERE id = $2::uuid AND deleted_at IS NULL`, p.Price, catalogItemID)
		}
	}
	return nil
}

func attachEffectiveSellPrices(ctx context.Context, ts appdb.TenantScope, items []CatalogItem, contactID string) error {
	priceTypeID, err := resolvePriceTypeIDForContact(ctx, ts, contactID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	inClause, inArgs := uuidInClauseFrom(ids, 2)
	args := append([]any{priceTypeID}, inArgs...)
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT catalog_item_id::text, price
		FROM business_catalog_item_price
		WHERE price_type_id = $1::uuid AND catalog_item_id IN (%s)`, inClause), args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	prices := make(map[string]float64)
	for rows.Next() {
		var id string
		var price float64
		if err := rows.Scan(&id, &price); err != nil {
			return err
		}
		prices[id] = price
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range items {
		if price, ok := prices[items[i].ID]; ok {
			items[i].EffectiveSellPrice = &price
		}
	}
	return nil
}

func uuidInClause(ids []string) (string, []any) {
	return uuidInClauseFrom(ids, 1)
}

func uuidInClauseFrom(ids []string, startIdx int) (string, []any) {
	parts := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("$%d::uuid", startIdx+i)
		args[i] = id
	}
	return strings.Join(parts, ","), args
}
