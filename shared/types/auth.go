package types

// AuthUser is extracted from the JWT/session on every authenticated request.
// Returned by the Encore auth handler and accessible via auth.Data().
// TenantID/TenantSchema are effective values (impersonation target when active).
type AuthUser struct {
	AccountID    string `json:"accountId"`
	TenantID     string `json:"tenantId"`
	TenantSchema string `json:"tenantSchema"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	Role         string `json:"role"` // "owner" | "staff" | "super_admin"
	SessionID    string `json:"sessionId"`

	HomeTenantID     string `json:"-"`
	HomeTenantSchema string `json:"-"`

	IsPlatformSession       bool   `json:"-"`
	Impersonating           bool   `json:"-"`
	ImpersonationTenantName string `json:"-"`
	ImpersonationTenantSlug string `json:"-"`
}

// HasEffectiveTenantContext is true when tenant-scoped APIs may run.
func (u *AuthUser) HasEffectiveTenantContext() bool {
	return u != nil && u.TenantSchema != ""
}

// CanPerformOwnerActions allows owner APIs (and super_admin while impersonating).
func (u *AuthUser) CanPerformOwnerActions() bool {
	if u == nil || u.TenantSchema == "" {
		return false
	}
	if u.Role == "owner" {
		return true
	}
	return u.Role == "super_admin" && u.Impersonating
}

// CanAccessInbox allows inbox/realtime for owner, staff, or impersonating platform admin.
func (u *AuthUser) CanAccessInbox() bool {
	if u == nil || u.TenantSchema == "" {
		return false
	}
	switch u.Role {
	case "owner", "staff":
		return true
	case "super_admin":
		return u.Impersonating
	default:
		return false
	}
}
