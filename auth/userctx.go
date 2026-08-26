package auth

import (
	"context"
	"fmt"
	"time"

	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/tenantaccess"
)

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
		u.ImpersonationScope = sess.ActAsScope
		if sess.ActAsScope == tenantaccess.ScopeLimited {
			u.ImpersonationModules = append([]string{}, sess.ActAsModules...)
		}
		if sess.ActAsGrantExpiresAt > 0 {
			t := time.Unix(sess.ActAsGrantExpiresAt, 0)
			u.ImpersonationExpiresAt = &t
		}
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

// clearImpersonationFields resets impersonation on session data.
func clearImpersonationFields(sess *SessionData) {
	sess.Impersonating = false
	sess.ActAsTenantID = ""
	sess.ActAsTenantSchema = ""
	sess.ActAsTenantName = ""
	sess.ActAsTenantSlug = ""
	sess.ActAsScope = ""
	sess.ActAsModules = nil
	sess.ActAsGrantID = ""
	sess.ActAsGrantExpiresAt = 0
}

// reconcileImpersonationGrant validates the active grant still allows impersonation.
// Clears impersonation in Redis when revoked, expired, or missing.
func reconcileImpersonationGrant(ctx context.Context, accountID, sessionID string, sess *SessionData) error {
	if sess == nil || !sess.Impersonating || sess.ActAsTenantID == "" {
		return nil
	}

	grant, err := tenantaccess.ActiveGrant(ctx, accountID, sess.ActAsTenantID)
	if err != nil {
		return fmt.Errorf("impersonation grant lookup: %w", err)
	}
	if grant == nil {
		clearImpersonationFields(sess)
		return updateSession(ctx, accountID, sessionID, *sess)
	}

	// Refresh scope/modules from DB (owner may have changed grant via new approval).
	sess.ActAsScope = grant.Scope
	sess.ActAsModules = grant.Modules
	sess.ActAsGrantID = grant.RequestID
	if grant.ExpiresAt != nil {
		sess.ActAsGrantExpiresAt = grant.ExpiresAt.Unix()
	} else {
		sess.ActAsGrantExpiresAt = 0
	}
	return updateSession(ctx, accountID, sessionID, *sess)
}
