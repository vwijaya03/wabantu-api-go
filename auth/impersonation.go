package auth

import (
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
func StartImpersonation(ctx context.Context, accountID, sessionID, tenantID string) error {
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

	var slug, name, schema, status string
	err = system.DB.QueryRow(ctx, `
		SELECT t.slug, t.name, tc.schema_name, t.status
		FROM tenant t
		JOIN tenant_company tc ON tc.tenant_id = t.id
		WHERE t.id = $1 AND t.deleted_at IS NULL`,
		tenantID,
	).Scan(&slug, &name, &schema, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return errs.NotFound("tenant not found")
	}
	if err != nil {
		return errs.Internal("tenant lookup failed")
	}
	if status != "active" {
		return errs.Forbidden("tenant is not active")
	}

	grant, err := tenantaccess.ActiveGrant(ctx, accountID, tenantID)
	if err != nil {
		return errs.Internal("grant lookup failed")
	}
	if grant == nil {
		return errs.Forbidden("minta persetujuan owner tenant terlebih dahulu sebelum pantau")
	}

	sess.Impersonating = true
	sess.ActAsTenantID = tenantID
	sess.ActAsTenantSchema = schema
	sess.ActAsTenantName = name
	sess.ActAsTenantSlug = slug
	sess.ActAsScope = grant.Scope
	sess.ActAsModules = grant.Modules
	sess.ActAsGrantID = grant.RequestID
	if grant.ExpiresAt != nil {
		sess.ActAsGrantExpiresAt = grant.ExpiresAt.Unix()
	} else {
		sess.ActAsGrantExpiresAt = 0
	}
	if err := updateSession(ctx, accountID, sessionID, *sess); err != nil {
		return errs.Internal(fmt.Sprintf("update session: %v", err))
	}
	return nil
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
