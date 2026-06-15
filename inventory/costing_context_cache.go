package inventory

import (
	"context"
	"database/sql"
	"errors"

	appErrs "encore.app/wabantu/shared/errs"
)

// costingContextLoader caches tenant defaults and per-item overrides within one tx/request.
type costingContextLoader struct {
	defaultsLoaded bool
	defaultMethod  string
	blockNegative  bool
	byItem         map[string]costingContext
}

func newCostingContextLoader() *costingContextLoader {
	return &costingContextLoader{byItem: make(map[string]costingContext)}
}

func (c *costingContextLoader) load(ctx context.Context, q querier, catalogItemID string) (costingContext, error) {
	if cc, ok := c.byItem[catalogItemID]; ok {
		return cc, nil
	}
	if !c.defaultsLoaded {
		var defMethod string
		var block bool
		err := q.QueryRowContext(ctx,
			`SELECT default_costing_method, block_negative_stock FROM inv_setting ORDER BY created_at LIMIT 1`).
			Scan(&defMethod, &block)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return costingContext{}, appErrs.Internal(err.Error())
		}
		if err == nil {
			c.defaultMethod = defMethod
			c.blockNegative = block
		} else {
			c.defaultMethod = CostingAverage
			c.blockNegative = true
		}
		c.defaultsLoaded = true
	}
	cc := costingContext{method: c.defaultMethod, blockNegative: c.blockNegative}
	var override sql.NullString
	oerr := q.QueryRowContext(ctx,
		`SELECT costing_method FROM inv_sku WHERE catalog_item_id = $1`, catalogItemID).Scan(&override)
	if oerr != nil && !errors.Is(oerr, sql.ErrNoRows) {
		return cc, appErrs.Internal(oerr.Error())
	}
	cc.method = effectiveCostingMethod(override.String, c.defaultMethod)
	c.byItem[catalogItemID] = cc
	return cc, nil
}
