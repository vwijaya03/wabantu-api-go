package templates

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"encore.dev/beta/auth"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

type InstalledTemplate struct {
	Kind      string          `json:"kind"`
	Surface   string          `json:"surface"`
	Slug      string          `json:"slug"`
	Version   string          `json:"version"`
	InstallID string          `json:"installId"`
	Manifest  json.RawMessage `json:"manifest,omitempty"`
}

type ListInstalledResponse struct {
	Items []InstalledTemplate `json:"items"`
}

type InstallTemplateResponse struct {
	InstallID string `json:"installId"`
	Kind      string `json:"kind"`
	Slug      string `json:"slug"`
}

type SetActiveTokensRequest struct {
	CustomTokens json.RawMessage `json:"customTokens"`
}

func tenantUser(ctx context.Context) (*types.AuthUser, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, appErrs.Unauthenticated("not authenticated")
	}
	return u, nil
}

//encore:api auth method=GET path=/api/v1/tenant/templates/installed tag:owner
func ListInstalledTemplates(ctx context.Context) (*ListInstalledResponse, error) {
	u, err := tenantUser(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := sysDB.Query(ctx, `
		SELECT ti.kind, ti.surface, tl.slug, tv.version, ti.id::text, tv.manifest_json
		FROM tenant_template_install ti
		JOIN template_listing tl ON tl.id = ti.listing_id
		JOIN template_version tv ON tv.id = ti.version_id
		WHERE ti.tenant_id = $1::uuid AND ti.is_active = true
		ORDER BY ti.surface`, u.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []InstalledTemplate
	for rows.Next() {
		var it InstalledTemplate
		if err := rows.Scan(&it.Kind, &it.Surface, &it.Slug, &it.Version, &it.InstallID, &it.Manifest); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return &ListInstalledResponse{Items: items}, rows.Err()
}

//encore:api auth method=POST path=/api/v1/tenant/templates/:slug/install tag:owner
func InstallTemplate(ctx context.Context, slug string) (*InstallTemplateResponse, error) {
	u, err := tenantUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := ensurePlatformTemplates(ctx); err != nil {
		return nil, appErrs.Internal("gagal memuat template")
	}
	slug = strings.TrimSpace(slug)
	var listingID, kind string
	var price int
	err = sysDB.QueryRow(ctx, `
		SELECT id::text, kind, price_idr FROM template_listing
		WHERE slug = $1 AND status = 'published'`, slug).Scan(&listingID, &kind, &price)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("template tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	if price > 0 {
		return nil, appErrs.BadRequest("template berbayar — beli dulu via marketplace")
	}
	var versionID, version string
	err = sysDB.QueryRow(ctx, `
		SELECT id::text, version FROM template_version
		WHERE listing_id = $1::uuid AND status = 'published'
		ORDER BY created_at DESC LIMIT 1`, listingID).Scan(&versionID, &version)
	if err != nil {
		return nil, err
	}
	if err := installTemplateSurfaces(ctx, u.TenantID, listingID, versionID, kind, u.AccountID); err != nil {
		return nil, err
	}
	var installID string
	surface := surfacesForKind(kind)[0]
	_ = sysDB.QueryRow(ctx, `
		SELECT id::text FROM tenant_template_install
		WHERE tenant_id = $1::uuid AND surface = $2`, u.TenantID, surface).Scan(&installID)
	_, _ = sysDB.Exec(ctx, `UPDATE template_listing SET install_count = install_count + 1 WHERE id = $1::uuid`, listingID)
	return &InstallTemplateResponse{InstallID: installID, Kind: kind, Slug: slug}, nil
}

//encore:api public method=GET path=/api/v1/templates/:slug/versions/:version
func GetTemplateVersion(ctx context.Context, slug, version string) (*TemplateDetail, error) {
	if err := ensurePlatformTemplates(ctx); err != nil {
		return nil, appErrs.Internal("gagal memuat template")
	}
	var d TemplateDetail
	var listingID string
	err := sysDB.QueryRow(ctx, `
		SELECT id::text, slug, kind, title, COALESCE(description, ''), price_idr
		FROM template_listing WHERE slug = $1 AND status = 'published'`, slug).
		Scan(&listingID, &d.Slug, &d.Kind, &d.Title, &d.Description, &d.PriceIDR)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("template tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	err = sysDB.QueryRow(ctx, `
		SELECT version, manifest_json FROM template_version
		WHERE listing_id = $1::uuid AND version = $2 AND status = 'published'`,
		listingID, version).Scan(&d.Version, &d.Manifest)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("versi tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}
