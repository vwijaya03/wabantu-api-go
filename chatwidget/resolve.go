package chatwidget

import (
	"context"
	"database/sql"
	"strings"

	"encore.dev/storage/sqldb"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/publictenant"
)

var systemDB = sqldb.Named("system")

func resolveTenantBySlug(ctx context.Context, slug string) (publictenant.TenantRef, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return publictenant.TenantRef{}, appErrs.BadRequest("tenant tidak valid")
	}
	var ref publictenant.TenantRef
	err := systemDB.QueryRow(ctx, `
		SELECT t.id::text, tc.schema_name, t.slug
		FROM tenant t
		JOIN tenant_company tc ON tc.tenant_id = t.id
		WHERE t.slug = $1 AND t.deleted_at IS NULL`, slug).
		Scan(&ref.TenantID, &ref.TenantSchema, &ref.Slug)
	if err == sql.ErrNoRows {
		return publictenant.TenantRef{}, appErrs.NotFound("tenant tidak ditemukan")
	}
	if err != nil {
		return publictenant.TenantRef{}, err
	}
	return ref, nil
}
