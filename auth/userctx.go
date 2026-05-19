package auth

import "encore.app/wabantu/shared/types"

// buildAuthUser maps Redis session → request context. TenantID/TenantSchema are the
// effective values (impersonation target when active).
func buildAuthUser(sess *SessionData, sessionID string) *types.AuthUser {
	u := &types.AuthUser{
		AccountID:        sess.AccountID,
		Role:             sess.Role,
		Email:            sess.Email,
		Name:             sess.Name,
		SessionID:        sessionID,
		HomeTenantID:     sess.TenantID,
		HomeTenantSchema: sess.TenantSchema,
	}

	if sess.Impersonating && sess.ActAsTenantSchema != "" {
		u.TenantID = sess.ActAsTenantID
		u.TenantSchema = sess.ActAsTenantSchema
		u.Impersonating = true
		u.ImpersonationTenantName = sess.ActAsTenantName
		u.ImpersonationTenantSlug = sess.ActAsTenantSlug
		return u
	}

	if sess.Role == roleSuperAdmin && sess.TenantID == "" {
		u.IsPlatformSession = true
		return u
	}

	u.TenantID = sess.TenantID
	u.TenantSchema = sess.TenantSchema
	return u
}
