package inventory

import (
	appdb "encore.app/wabantu/shared/db"
	"context"
	"database/sql"
	"errors"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

type SkuConfig struct {
	CatalogItemID  string  `json:"catalogItemId"`
	TrackStock     bool    `json:"trackStock"`
	IsBundle       bool    `json:"isBundle"`
	CostingMethod  *string `json:"costingMethod,omitempty"` // null = inherit tenant default
	TrackBatch     bool    `json:"trackBatch"`
	TrackSerial    bool    `json:"trackSerial"`
	TrackExpiry    bool    `json:"trackExpiry"`
	BaseUOM        *string `json:"baseUom,omitempty"`
	EffectiveMethod string `json:"effectiveMethod"`
}

type UpdateSkuParams struct {
	TrackStock    *bool   `json:"trackStock"`
	CostingMethod *string `json:"costingMethod"` // "", "inherit", or fifo/lifo/average
	TrackBatch    *bool   `json:"trackBatch"`
	TrackSerial   *bool   `json:"trackSerial"`
	TrackExpiry   *bool   `json:"trackExpiry"`
	BaseUOM       *string `json:"baseUom"`
}

//encore:api auth method=GET path=/api/v1/inventory/skus/:catalogItemID
func GetSku(ctx context.Context, catalogItemID string) (*SkuConfig, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()
	return loadSkuConfig(ctx, sch, pool, catalogItemID)
}

//encore:api auth method=PATCH path=/api/v1/inventory/skus/:catalogItemID
func UpdateSku(ctx context.Context, catalogItemID string, p *UpdateSkuParams) (*SkuConfig, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	if err := validateCatalogItem(ctx, sch, pool, catalogItemID); err != nil {
		return nil, err
	}
	if err := ensureSku(ctx, sch, pool, catalogItemID); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	methodChanged := false
	if p.CostingMethod != nil {
		raw := strings.ToLower(strings.TrimSpace(*p.CostingMethod))
		if raw == "" || raw == "inherit" {
			if _, err := qexec(ctx, sch, pool,
				`UPDATE inv_sku SET costing_method = NULL, updated_at = now() WHERE catalog_item_id = $1`, catalogItemID); err != nil {
				return nil, appErrs.Internal(err.Error())
			}
			methodChanged = true
		} else {
			method, ok := normalizeCostingMethod(raw)
			if !ok {
				return nil, appErrs.BadRequest("metode HPP harus fifo, lifo, average, atau inherit")
			}
			if _, err := qexec(ctx, sch, pool,
				`UPDATE inv_sku SET costing_method = $2, updated_at = now() WHERE catalog_item_id = $1`, catalogItemID, method); err != nil {
				return nil, appErrs.Internal(err.Error())
			}
			methodChanged = true
		}
	}
	if err := updateSkuBoolCols(ctx, sch, pool, catalogItemID, p); err != nil {
		return nil, err
	}

	// Changing the costing method requires recomputing existing layers/balances.
	if methodChanged {
		if _, err := RecalculateHPP(ctx, &RecalcParams{CatalogItemID: catalogItemID}); err != nil {
			return nil, err
		}
	}
	return loadSkuConfig(ctx, sch, pool, catalogItemID)
}

func updateSkuBoolCols(ctx context.Context, sch appdb.SchemaSQL, q querier, catalogItemID string, p *UpdateSkuParams) error {
	type col struct {
		name string
		val  *bool
	}
	cols := []col{
		{"track_stock", p.TrackStock},
		{"track_batch", p.TrackBatch},
		{"track_serial", p.TrackSerial},
		{"track_expiry", p.TrackExpiry},
	}
	for _, c := range cols {
		if c.val == nil {
			continue
		}
		if _, err := qexec(ctx, sch, q,
			"UPDATE inv_sku SET "+c.name+" = $2, updated_at = now() WHERE catalog_item_id = $1",
			catalogItemID, *c.val); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	if p.BaseUOM != nil {
		if _, err := qexec(ctx, sch, q,
			`UPDATE inv_sku SET base_uom = $2, updated_at = now() WHERE catalog_item_id = $1`,
			catalogItemID, nullStr(*p.BaseUOM)); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	return nil
}

func loadSkuConfig(ctx context.Context, sch appdb.SchemaSQL, q querier, catalogItemID string) (*SkuConfig, error) {
	var s SkuConfig
	s.CatalogItemID = catalogItemID
	var method, baseUOM sql.NullString
	err := qrow(ctx, sch, q, `
		SELECT track_stock, is_bundle, costing_method, track_batch, track_serial, track_expiry, base_uom
		FROM inv_sku WHERE catalog_item_id = $1`, catalogItemID).
		Scan(&s.TrackStock, &s.IsBundle, &method, &s.TrackBatch, &s.TrackSerial, &s.TrackExpiry, &baseUOM)
	if errors.Is(err, sql.ErrNoRows) {
		// Item not yet tracked: report defaults (inherit tenant method).
		s.TrackStock = false
	} else if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if method.Valid && method.String != "" {
		m := method.String
		s.CostingMethod = &m
	}
	if baseUOM.Valid && baseUOM.String != "" {
		b := baseUOM.String
		s.BaseUOM = &b
	}

	var tenantDefault string
	_ = qrow(ctx, sch, q,
		`SELECT default_costing_method FROM inv_setting ORDER BY created_at LIMIT 1`).Scan(&tenantDefault)
	override := ""
	if s.CostingMethod != nil {
		override = *s.CostingMethod
	}
	s.EffectiveMethod = effectiveCostingMethod(override, tenantDefault)
	return &s, nil
}
