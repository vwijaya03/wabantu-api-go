package tenant

import (
	"context"

	"encore.dev/beta/auth"

	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/tenantschema"
	"encore.app/wabantu/shared/types"
)

// TenantReadinessResponse reports whether the current tenant schema is safe for dashboard queries.
type TenantReadinessResponse struct {
	Ready           bool `json:"ready"`
	BaseProvisioned bool `json:"baseProvisioned"`
	CloudReady      bool `json:"cloudReady"`
	PatchVersion    int  `json:"patchVersion"`
	PatchCurrent    int  `json:"patchCurrent"`
	Migrating       bool `json:"migrating"`
}

//encore:api auth method=GET path=/api/v1/tenant/readiness
func GetTenantReadiness(ctx context.Context) (*TenantReadinessResponse, error) {
	u, err := readinessUser()
	if err != nil {
		return nil, err
	}
	if err := ValidateTenantSchemaName(u.TenantSchema); err != nil {
		return &TenantReadinessResponse{}, nil
	}

	pool := DataDB.Stdlib()
	base, err := tenantSchemaBaseProvisioned(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	cloud, err := tenantschema.CloudTenantReady(ctx, pool, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	patchVer, err := getTenantSchemaPatchVersion(ctx, u.TenantID)
	if err != nil {
		return nil, err
	}
	migrating, err := schemaMigrationJobActive(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}

	ready := base && cloud && patchVer >= CurrentSchemaPatchVersion && !migrating
	return &TenantReadinessResponse{
		Ready:           ready,
		BaseProvisioned: base,
		CloudReady:      cloud,
		PatchVersion:    patchVer,
		PatchCurrent:    CurrentSchemaPatchVersion,
		Migrating:       migrating,
	}, nil
}

func readinessUser() (*types.AuthUser, error) {
	data, ok := auth.Data().(*types.AuthUser)
	if !ok || data == nil {
		return nil, apperr.Unauthenticated("not authenticated")
	}
	return data, nil
}
