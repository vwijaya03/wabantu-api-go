package storefront

import (
	"context"
	"database/sql"
	"encoding/json"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/publictenant"
)

type PublicStoreResponse struct {
	Enabled     bool            `json:"enabled"`
	StoreTitle  string          `json:"storeTitle,omitempty"`
	Description string          `json:"description,omitempty"`
	SEOTitle    string          `json:"seoTitle,omitempty"`
	SEODesc     string          `json:"seoDescription,omitempty"`
	FeaturedIDs json.RawMessage `json:"featuredProductIds,omitempty"`
	Tokens      json.RawMessage `json:"tokens,omitempty"`
}

type ProductSummary struct {
	ID       string  `json:"id"`
	Slug     string  `json:"slug"`
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	ImageURL string  `json:"imageUrl,omitempty"`
}

type ListProductsParams struct {
	Q    string `query:"q"`
	Page int    `query:"page"`
}

type ListProductsResponse struct {
	Items []ProductSummary `json:"items"`
	Page  int              `json:"page"`
}

//encore:api public method=GET path=/api/v1/public/store/:tenantSlug
func GetPublicStore(ctx context.Context, tenantSlug string) (*PublicStoreResponse, error) {
	return publictenant.RunPublicTenant(ctx, tenantSlug, resolveTenantBySlug, func(ref publictenant.TenantRef) (*PublicStoreResponse, error) {
		ts, err := openTenant(ctx, ref.TenantSchema)
		if err != nil {
			return nil, err
		}
		var resp PublicStoreResponse
		var featured, tokens []byte
		err = ts.QueryRowContext(ctx, `
			SELECT is_enabled, COALESCE(store_title,''), COALESCE(store_description,''),
			       COALESCE(seo_title,''), COALESCE(seo_description,''), featured_product_ids, custom_tokens
			FROM `+ts.T("storefront_config")+` LIMIT 1`).
			Scan(&resp.Enabled, &resp.StoreTitle, &resp.Description, &resp.SEOTitle, &resp.SEODesc, &featured, &tokens)
		if err == sql.ErrNoRows {
			return &PublicStoreResponse{Enabled: false}, nil
		}
		if err != nil {
			return nil, err
		}
		if len(featured) > 0 {
			resp.FeaturedIDs = featured
		}
		if len(tokens) > 0 {
			resp.Tokens = tokens
		}
		return &resp, nil
	})
}

//encore:api public method=GET path=/api/v1/public/store/:tenantSlug/products
func ListPublicProducts(ctx context.Context, tenantSlug string, p *ListProductsParams) (*ListProductsResponse, error) {
	return publictenant.RunPublicTenant(ctx, tenantSlug, resolveTenantBySlug, func(ref publictenant.TenantRef) (*ListProductsResponse, error) {
		ts, err := openTenant(ctx, ref.TenantSchema)
		if err != nil {
			return nil, err
		}
		page := p.Page
		if page < 1 {
			page = 1
		}
		offset := (page - 1) * 24
		q := `%` + p.Q + `%`
		rows, err := ts.QueryContext(ctx, `
			SELECT id::text, COALESCE(slug,''), name, sell_price, COALESCE(image_url,'')
			FROM `+ts.T("business_catalog_item")+`
			WHERE deleted_at IS NULL AND is_active = true AND is_storefront_visible = true
			  AND ($1 = '' OR name ILIKE $2 OR slug ILIKE $2)
			ORDER BY name ASC LIMIT 24 OFFSET $3`, p.Q, q, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var items []ProductSummary
		for rows.Next() {
			var it ProductSummary
			if err := rows.Scan(&it.ID, &it.Slug, &it.Name, &it.Price, &it.ImageURL); err != nil {
				return nil, err
			}
			items = append(items, it)
		}
		return &ListProductsResponse{Items: items, Page: page}, rows.Err()
	})
}

//encore:api public method=GET path=/api/v1/public/store/:tenantSlug/products/:slug
func GetPublicProduct(ctx context.Context, tenantSlug, slug string) (*ProductSummary, error) {
	return publictenant.RunPublicTenant(ctx, tenantSlug, resolveTenantBySlug, func(ref publictenant.TenantRef) (*ProductSummary, error) {
		ts, err := openTenant(ctx, ref.TenantSchema)
		if err != nil {
			return nil, err
		}
		var it ProductSummary
		err = ts.QueryRowContext(ctx, `
			SELECT id::text, COALESCE(slug,''), name, sell_price, COALESCE(image_url,'')
			FROM `+ts.T("business_catalog_item")+`
			WHERE deleted_at IS NULL AND is_active = true AND is_storefront_visible = true AND slug = $1`, slug).
			Scan(&it.ID, &it.Slug, &it.Name, &it.Price, &it.ImageURL)
		if err == sql.ErrNoRows {
			return nil, appErrs.NotFound("produk tidak ditemukan")
		}
		if err != nil {
			return nil, err
		}
		return &it, nil
	})
}
