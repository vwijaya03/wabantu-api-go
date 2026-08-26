package types

import "encore.dev/beta/errs"

// TenantNavModuleIDs mirror web-frontend TENANT_NAV_SECTIONS (plus platform for SA console).
var TenantNavModuleIDs = []string{
	"main", "sales", "inventory", "finance", "ai", "org", "advanced",
}

// ValidTenantModule reports whether moduleID is a known tenant nav module.
func ValidTenantModule(moduleID string) bool {
	for _, m := range TenantNavModuleIDs {
		if m == moduleID {
			return true
		}
	}
	return false
}

// RequireModule enforces impersonation module scope. No-op unless impersonating with a limited grant.
func (u *AuthUser) RequireModule(moduleID string) error {
	if u == nil || !u.Impersonating {
		return nil
	}
	if len(u.ImpersonationModules) == 0 {
		return nil
	}
	for _, m := range u.ImpersonationModules {
		if m == moduleID {
			return nil
		}
	}
	return &errs.Error{
		Code:    errs.PermissionDenied,
		Message: "akses modul tidak diizinkan untuk sesi pantau ini",
	}
}
