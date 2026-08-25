package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"encore.app/wabantu/system"
)

// reconcileSessionTenant reloads the effective tenant schema from the system DB on
// every authenticated request so stale Redis sessions cannot serve the wrong tenant.
func reconcileSessionTenant(ctx context.Context, sess *SessionData) error {
	if sess == nil {
		return fmt.Errorf("nil session")
	}
	if sess.Impersonating && sess.ActAsTenantID != "" {
		var schema, status string
		err := system.DB.QueryRow(ctx, `
			SELECT tc.schema_name, t.status
			FROM tenant t
			JOIN tenant_company tc ON tc.tenant_id = t.id
			WHERE t.id = $1 AND t.deleted_at IS NULL`,
			sess.ActAsTenantID,
		).Scan(&schema, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("impersonation tenant not found")
		}
		if err != nil {
			return fmt.Errorf("impersonation tenant lookup: %w", err)
		}
		if status != "active" {
			return fmt.Errorf("impersonation tenant inactive")
		}
		sess.ActAsTenantSchema = schema
		return nil
	}
	if sess.Role == roleSuperAdmin && sess.TenantID == "" {
		return nil
	}
	if sess.TenantID == "" {
		return fmt.Errorf("tenant id missing in session")
	}
	var schema, status string
	err := system.DB.QueryRow(ctx, `
		SELECT tc.schema_name, t.status
		FROM tenant t
		JOIN tenant_company tc ON tc.tenant_id = t.id
		WHERE t.id = $1 AND t.deleted_at IS NULL`,
		sess.TenantID,
	).Scan(&schema, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("tenant not found")
	}
	if err != nil {
		return fmt.Errorf("tenant lookup: %w", err)
	}
	if status != "active" {
		return fmt.Errorf("tenant inactive")
	}
	sess.TenantSchema = schema
	return nil
}
