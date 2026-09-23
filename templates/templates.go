package templates

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"encore.dev/storage/sqldb"

	appErrs "encore.app/wabantu/shared/errs"
)

var sysDB = sqldb.Named("system")

type ListingSummary struct {
	Slug        string `json:"slug"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	PriceIDR    int    `json:"priceIdr"`
}

type ListTemplatesParams struct {
	Kind string `query:"kind"`
}

type ListTemplatesResponse struct {
	Items []ListingSummary `json:"items"`
}

type TemplateDetail struct {
	Slug        string          `json:"slug"`
	Kind        string          `json:"kind"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	PriceIDR    int             `json:"priceIdr"`
	Version     string          `json:"version"`
	Manifest    json.RawMessage `json:"manifest"`
}

//encore:api public method=GET path=/api/v1/templates
func ListTemplates(ctx context.Context, p *ListTemplatesParams) (*ListTemplatesResponse, error) {
	if err := ensurePlatformTemplates(ctx); err != nil {
		return nil, appErrs.Internal("gagal memuat template")
	}
	kind := strings.TrimSpace(p.Kind)
	q := `
		SELECT slug, kind, title, COALESCE(description, ''), price_idr
		FROM template_listing
		WHERE status = 'published'`
	args := []any{}
	if kind != "" {
		q += ` AND kind = $1`
		args = append(args, kind)
	}
	q += ` ORDER BY install_count DESC, title ASC`
	rows, err := sysDB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ListingSummary
	for rows.Next() {
		var it ListingSummary
		if err := rows.Scan(&it.Slug, &it.Kind, &it.Title, &it.Description, &it.PriceIDR); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return &ListTemplatesResponse{Items: items}, rows.Err()
}

//encore:api public method=GET path=/api/v1/templates/:slug
func GetTemplate(ctx context.Context, slug string) (*TemplateDetail, error) {
	if err := ensurePlatformTemplates(ctx); err != nil {
		return nil, appErrs.Internal("gagal memuat template")
	}
	slug = strings.TrimSpace(slug)
	var d TemplateDetail
	var listingID string
	err := sysDB.QueryRow(ctx, `
		SELECT id::text, slug, kind, title, COALESCE(description, ''), price_idr
		FROM template_listing
		WHERE slug = $1 AND status = 'published'`, slug).
		Scan(&listingID, &d.Slug, &d.Kind, &d.Title, &d.Description, &d.PriceIDR)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("template tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	err = sysDB.QueryRow(ctx, `
		SELECT version, manifest_json
		FROM template_version
		WHERE listing_id = $1::uuid AND status = 'published'
		ORDER BY created_at DESC LIMIT 1`, listingID).
		Scan(&d.Version, &d.Manifest)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("versi template tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}
