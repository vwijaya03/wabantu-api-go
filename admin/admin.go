package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("tenant")

// ---------- Types ----------

type Tenant struct {
	ID           string     `json:"id"`
	CompanyName  string     `json:"companyName"`
	SchemaName   string     `json:"schemaName"`
	PlanTier     string     `json:"planTier"`
	IsActive     bool       `json:"isActive"`
	CreatedAt    time.Time  `json:"createdAt"`
	OwnerEmail   string     `json:"ownerEmail,omitempty"`
}

type TenantDetail struct {
	Tenant
	AccountCount      int `json:"accountCount"`
	ConversationCount int `json:"conversationCount"`
	MessageCount      int `json:"messageCount"`
}

type ListTenantsResponse struct {
	Tenants []Tenant `json:"tenants"`
	Total   int      `json:"total"`
}

type TenantDetailResponse struct {
	Tenant TenantDetail `json:"tenant"`
}

type ImpersonateResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	TenantID  string    `json:"tenantId"`
}

type StopImpersonationResponse struct {
	Message string `json:"message"`
}

// ---------- Endpoints ----------

// ListTenants returns all tenants in the system.
//
//encore:api auth method=GET path=/admin/tenants tag:super_admin
func ListTenants(ctx context.Context) (*ListTenantsResponse, error) {
	if err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}

	rows, err := db.Query(ctx, `
		SELECT tc.id, tc.company_name, tc.schema_name, tc.plan_tier, tc.is_active, tc.created_at,
			COALESCE(a.email, '') as owner_email
		FROM tenant_company tc
		LEFT JOIN account a ON a.tenant_id = tc.id AND a.role = 'owner'
		ORDER BY tc.created_at DESC
	`)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "query failed"}
	}
	defer rows.Close()

	var tenants []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.CompanyName, &t.SchemaName, &t.PlanTier, &t.IsActive, &t.CreatedAt, &t.OwnerEmail); err != nil {
			return nil, &errs.Error{Code: errs.Internal, Message: "scan failed"}
		}
		tenants = append(tenants, t)
	}
	if err := rows.Err(); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "rows iteration failed"}
	}

	return &ListTenantsResponse{Tenants: tenants, Total: len(tenants)}, nil
}

// GetTenant returns detailed info about a specific tenant.
//
//encore:api auth method=GET path=/admin/tenant/:id tag:super_admin
func GetTenant(ctx context.Context, id string) (*TenantDetailResponse, error) {
	if err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}

	var t TenantDetail
	err := db.QueryRow(ctx, `
		SELECT tc.id, tc.company_name, tc.schema_name, tc.plan_tier, tc.is_active, tc.created_at,
			COALESCE(a.email, '') as owner_email
		FROM tenant_company tc
		LEFT JOIN account a ON a.tenant_id = tc.id AND a.role = 'owner'
		WHERE tc.id = $1
	`, id).Scan(&t.ID, &t.CompanyName, &t.SchemaName, &t.PlanTier, &t.IsActive, &t.CreatedAt, &t.OwnerEmail)
	if err == sql.ErrNoRows {
		return nil, &errs.Error{Code: errs.NotFound, Message: "tenant not found"}
	}
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "query failed"}
	}

	// Counts from tenant schema
	_ = db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q.account`, t.SchemaName)).Scan(&t.AccountCount)
	_ = db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q.conversation`, t.SchemaName)).Scan(&t.ConversationCount)
	_ = db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q.message`, t.SchemaName)).Scan(&t.MessageCount)

	return &TenantDetailResponse{Tenant: t}, nil
}

// Impersonate creates an impersonation session for a tenant.
//
//encore:api auth method=POST path=/admin/impersonate/:tenantId tag:super_admin
func Impersonate(ctx context.Context, tenantId string) (*ImpersonateResponse, error) {
	if err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}

	uid, _ := auth.UserID()
	adminID := string(uid)

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "token generation failed"}
	}
	token := hex.EncodeToString(tokenBytes)
	expiresAt := time.Now().Add(2 * time.Hour)

	_, err := db.Exec(ctx, `
		INSERT INTO admin_session (admin_account_id, tenant_id, session_token, expires_at, created_at)
		VALUES ($1, $2, $3, $4, NOW())
	`, adminID, tenantId, token, expiresAt)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "session creation failed"}
	}

	_, err = db.Exec(ctx, `
		INSERT INTO impersonation_log (admin_account_id, tenant_id, action, created_at)
		VALUES ($1, $2, 'start', NOW())
	`, adminID, tenantId)
	if err != nil {
		rlog.Error("failed to log impersonation start", "err", err)
	}

	rlog.Info("impersonation started", "adminId", adminID, "tenantId", tenantId)
	return &ImpersonateResponse{
		Token:     token,
		ExpiresAt: expiresAt,
		TenantID:  tenantId,
	}, nil
}

// StopImpersonation ends the current impersonation session.
//
//encore:api auth method=POST path=/admin/stop-impersonation tag:super_admin
func StopImpersonation(ctx context.Context) (*StopImpersonationResponse, error) {
	if err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}

	uid, _ := auth.UserID()
	adminID := string(uid)

	_, err := db.Exec(ctx, `
		UPDATE admin_session SET ended_at = NOW()
		WHERE admin_account_id = $1 AND ended_at IS NULL
	`, adminID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to end session"}
	}

	_, err = db.Exec(ctx, `
		INSERT INTO impersonation_log (admin_account_id, tenant_id, action, created_at)
		VALUES ($1, (
			SELECT tenant_id FROM admin_session
			WHERE admin_account_id = $1 ORDER BY created_at DESC LIMIT 1
		), 'stop', NOW())
	`, adminID)
	if err != nil {
		rlog.Error("failed to log impersonation stop", "err", err)
	}

	rlog.Info("impersonation stopped", "adminId", adminID)
	return &StopImpersonationResponse{Message: "Impersonation session ended"}, nil
}

// ---------- Helpers ----------

func requireSuperAdmin(ctx context.Context) error {
	userData, ok := auth.Data().(*types.AuthUser)
	if !ok || userData == nil {
		return &errs.Error{Code: errs.Unauthenticated, Message: "not authenticated"}
	}
	if userData.Role != "super_admin" {
		return &errs.Error{Code: errs.PermissionDenied, Message: "super admin access required"}
	}
	return nil
}
