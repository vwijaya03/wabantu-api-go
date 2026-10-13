package auth

import (
	"encore.app/wabantu/internal/adminauth"
	"time"
	"context"
	"database/sql"
	"errors"
	"fmt"

	"encore.app/wabantu/shared/errs"
	"encore.app/wabantu/system"
	"encore.app/wabantu/tenantaccess"
)

// StartImpersonation switches the Redis session to act as the given tenant (super_admin only).
// Requires an active owner-approved grant (tenant access consent).
// ImpersonationToken is returned by StartImpersonation; the admin session is NOT modified.
type ImpersonationToken struct {
	AccessToken string
	ExpiresAt   time.Time
	SessionID   string
	GrantID     string
	AdminEmail  string
}

// StartImpersonation validates the approved grant and mints a SEPARATE short-lived
// impersonation session+token (typ "impersonation", 30 min < admin 2 h). That token acts
// as the tenant on client endpoints and is rejected at /admin/*.
func StartImpersonation(ctx context.Context, accountID, sessionID, tenantID string) (*ImpersonationToken, error) {
	tok, err := startImpersonation(ctx, accountID, sessionID, tenantID)
	return tok, err
}

func startImpersonation(ctx context.Context, accountID, sessionID, tenantID string) (*ImpersonationToken, error) {
	sess, err := getSession(ctx, accountID, sessionID)
	if err != nil {
		return nil, errs.Internal("session lookup failed")
	}
	if sess == nil {
		return nil, errs.Unauthenticated("session expired")
	}
	if sess.Role != roleSuperAdmin {
		return nil, errs.Forbidden("super admin required")
	}

	var slug, name, schema, status string
	err = system.DB.QueryRow(ctx, `
		SELECT t.slug, t.name, tc.schema_name, t.status
		FROM tenant t
		JOIN tenant_company tc ON tc.tenant_id = t.id
		WHERE t.id = $1 AND t.deleted_at IS NULL`,
		tenantID,
	).Scan(&slug, &name, &schema, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.NotFound("tenant not found")
	}
	if err != nil {
		return nil, errs.Internal("tenant lookup failed")
	}
	if status != "active" {
		return nil, errs.Forbidden("tenant is not active")
	}

	grant, err := tenantaccess.ActiveGrant(ctx, accountID, tenantID)
	if err != nil {
		return nil, errs.Internal("grant lookup failed")
	}
	if grant == nil {
		return nil, errs.Forbidden("minta persetujuan owner tenant terlebih dahulu sebelum pantau")
	}

	if adminauth.NormalizeKind(sess.Kind) != adminauth.KindAdmin {
		return nil, errs.Forbidden("admin session required")
	}
	imp := SessionData{
		AccountID: sess.AccountID, Role: sess.Role, Email: sess.Email, Name: sess.Name,
		Kind: adminauth.KindImpersonation, IssuedAt: time.Now().Unix(),
		Impersonating: true, ActAsTenantID: tenantID, ActAsTenantSchema: schema,
		ActAsTenantName: name, ActAsTenantSlug: slug, ActAsScope: grant.Scope,
		ActAsModules: grant.Modules, ActAsGrantID: grant.RequestID,
	}
	if grant.ExpiresAt != nil {
		imp.ActAsGrantExpiresAt = grant.ExpiresAt.Unix()
	}
	created, err := createSession(ctx, imp)
	if err != nil {
		return nil, errs.Internal("create impersonation session failed")
	}
	if sess.ImpersonationSessionID != "" {
		_ = destroySession(ctx, accountID, sess.ImpersonationSessionID)
	}
	sess.ImpersonationSessionID = created.SessionID
	if err := updateSession(ctx, accountID, sessionID, *sess); err != nil {
		_ = destroySession(ctx, accountID, created.SessionID)
		return nil, errs.Internal("update session failed")
	}
	token, expiresIn, err := signJWTKind(accountID, created.SessionID, adminauth.KindImpersonation)
	if err != nil {
		_ = destroySession(ctx, accountID, created.SessionID)
		return nil, errs.Internal("sign token failed")
	}
	return &ImpersonationToken{
		AccessToken: token, ExpiresAt: time.Now().Add(time.Duration(expiresIn) * time.Second).UTC(),
		SessionID: created.SessionID, GrantID: grant.RequestID, AdminEmail: sess.Email,
	}, nil
}

// StopImpersonation clears impersonation on the current Redis session.
func StopImpersonation(ctx context.Context, accountID, sessionID string) error {
	sess, err := getSession(ctx, accountID, sessionID)
	if err != nil {
		return errs.Internal("session lookup failed")
	}
	if sess == nil {
		return errs.Unauthenticated("session expired")
	}
	if sess.Role != roleSuperAdmin {
		return errs.Forbidden("super admin required")
	}

	if sess.ImpersonationSessionID != "" {
		_ = destroySession(ctx, accountID, sess.ImpersonationSessionID)
		sess.ImpersonationSessionID = ""
	}
	sess.Impersonating = false
	sess.ActAsTenantID = ""
	sess.ActAsTenantSchema = ""
	sess.ActAsTenantName = ""
	sess.ActAsTenantSlug = ""
	sess.ActAsScope = ""
	sess.ActAsModules = nil
	sess.ActAsGrantID = ""
	sess.ActAsGrantExpiresAt = 0
	if err := updateSession(ctx, accountID, sessionID, *sess); err != nil {
		return errs.Internal(fmt.Sprintf("update session: %v", err))
	}
	return nil
}
