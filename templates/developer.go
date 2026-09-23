package templates

import (
	"context"
	"database/sql"
	"strings"

	"encore.dev/beta/auth"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

type DeveloperProfile struct {
	DisplayName string `json:"displayName"`
	Slug        string `json:"slug"`
	Bio         string `json:"bio,omitempty"`
	WebsiteURL  string `json:"websiteUrl,omitempty"`
	KYCStatus   string `json:"kycStatus"`
}

type RegisterDeveloperRequest struct {
	DisplayName string `json:"displayName"`
	Slug        string `json:"slug"`
	Bio         string `json:"bio,omitempty"`
	WebsiteURL  string `json:"websiteUrl,omitempty"`
}

//encore:api auth method=POST path=/api/v1/developer/register tag:owner
func RegisterDeveloper(ctx context.Context, req *RegisterDeveloperRequest) (*DeveloperProfile, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, appErrs.Unauthenticated("not authenticated")
	}
	slug := strings.TrimSpace(req.Slug)
	if slug == "" || req.DisplayName == "" {
		return nil, appErrs.BadRequest("displayName dan slug wajib")
	}
	var prof DeveloperProfile
	err := sysDB.QueryRow(ctx, `
		INSERT INTO developer_account (account_id, tenant_id, display_name, slug, bio, website_url)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)
		ON CONFLICT (tenant_id) DO UPDATE
		SET display_name = EXCLUDED.display_name, slug = EXCLUDED.slug,
		    bio = EXCLUDED.bio, website_url = EXCLUDED.website_url, updated_at = now()
		RETURNING display_name, slug, COALESCE(bio,''), COALESCE(website_url,''), kyc_status`,
		u.AccountID, u.TenantID, req.DisplayName, slug, req.Bio, req.WebsiteURL).
		Scan(&prof.DisplayName, &prof.Slug, &prof.Bio, &prof.WebsiteURL, &prof.KYCStatus)
	if err != nil {
		return nil, err
	}
	return &prof, nil
}

//encore:api auth method=GET path=/api/v1/developer/me tag:owner
func GetDeveloperMe(ctx context.Context) (*DeveloperProfile, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, appErrs.Unauthenticated("not authenticated")
	}
	var prof DeveloperProfile
	err := sysDB.QueryRow(ctx, `
		SELECT display_name, slug, COALESCE(bio,''), COALESCE(website_url,''), kyc_status
		FROM developer_account WHERE tenant_id = $1::uuid`, u.TenantID).
		Scan(&prof.DisplayName, &prof.Slug, &prof.Bio, &prof.WebsiteURL, &prof.KYCStatus)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("belum terdaftar sebagai developer")
	}
	if err != nil {
		return nil, err
	}
	return &prof, nil
}
