package inventory

import (
	"context"
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

type ListConfigItemsParams struct {
	Q        string `query:"q"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ConfigCatalogItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ExternalCode string `json:"externalCode"`
	TrackStock   bool   `json:"trackStock"`
}

type ListConfigItemsResponse struct {
	Items    []ConfigCatalogItem `json:"items"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"pageSize"`
}

//encore:api auth method=GET path=/api/v1/inventory/config-items
func ListConfigItems(ctx context.Context, p *ListConfigItemsParams) (*ListConfigItemsResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	if p == nil {
		p = &ListConfigItemsParams{}
	}
	page, pageSize := p.Page, p.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	// Exclude bundle parents — stok bundle diambil dari komponen anak.
	baseWhere := `
		FROM business_catalog_item ci
		LEFT JOIN inv_sku s ON s.catalog_item_id = ci.id
		WHERE ci.deleted_at IS NULL
		  AND NOT COALESCE(s.is_bundle, false)
		  AND NOT EXISTS (
		    SELECT 1 FROM inv_bundle_component bc WHERE bc.parent_catalog_item_id = ci.id
		  )`
	where := baseWhere
	args := []any{}
	idx := 1
	if q := strings.TrimSpace(p.Q); q != "" {
		where += fmt.Sprintf(` AND (ci.name ILIKE $%d OR COALESCE(ci.external_code,'') ILIKE $%d)`, idx, idx)
		args = append(args, "%"+q+"%")
		idx++
	}

	var total int
	if err := qrow(ctx, sch, pool, `SELECT COUNT(*)`+where, args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, err := qquery(ctx, sch, pool, fmt.Sprintf(`
		SELECT ci.id::text, COALESCE(ci.name,''), COALESCE(ci.external_code,''),
		       COALESCE(s.track_stock, false)
		%s
		ORDER BY ci.name
		LIMIT $%d OFFSET $%d`, where, idx, idx+1), listArgs...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	items := []ConfigCatalogItem{}
	for rows.Next() {
		var it ConfigCatalogItem
		if err := rows.Scan(&it.ID, &it.Name, &it.ExternalCode, &it.TrackStock); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	return &ListConfigItemsResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}
