package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("system")

// ---------- Types ----------

type Tenant struct {
	ID          string    `json:"id"`
	CompanyName string    `json:"companyName"`
	SchemaName  string    `json:"schemaName"`
	PlanTier    string    `json:"planTier"`
	IsActive    bool      `json:"isActive"`
	CreatedAt   time.Time `json:"createdAt"`
	OwnerEmail  string    `json:"ownerEmail,omitempty"`
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
//encore:api auth method=GET path=/api/v1/admin/tenants tag:super_admin
func ListTenants(ctx context.Context) (*ListTenantsResponse, error) {
	if err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}

	rows, err := db.Query(ctx, `
		SELECT t.id, t.name, tc.schema_name, t.status, t.created_at,
			COALESCE(ta.email, '') AS owner_email
		FROM tenant t
		JOIN tenant_company tc ON tc.tenant_id = t.id
		LEFT JOIN tenant_account ta ON ta.tenant_id = t.id AND ta.role = 'owner' AND ta.deleted_at IS NULL
		WHERE t.deleted_at IS NULL
		ORDER BY t.created_at DESC
	`)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "query failed"}
	}
	defer rows.Close()

	var tenants []Tenant
	for rows.Next() {
		var t Tenant
		var status string
		if err := rows.Scan(&t.ID, &t.CompanyName, &t.SchemaName, &status, &t.CreatedAt, &t.OwnerEmail); err != nil {
			return nil, &errs.Error{Code: errs.Internal, Message: "scan failed"}
		}
		t.PlanTier = "starter"
		t.IsActive = status == "active"
		tenants = append(tenants, t)
	}
	if err := rows.Err(); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "rows iteration failed"}
	}
	if tenants == nil {
		tenants = []Tenant{}
	}

	return &ListTenantsResponse{Tenants: tenants, Total: len(tenants)}, nil
}

// GetTenant returns detailed info about a specific tenant.
//
//encore:api auth method=GET path=/api/v1/admin/tenant/:id tag:super_admin
func GetTenant(ctx context.Context, id string) (*TenantDetailResponse, error) {
	if err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}

	var t TenantDetail
	var status string
	err := db.QueryRow(ctx, `
		SELECT t.id, t.name, tc.schema_name, t.status, t.created_at,
			COALESCE(ta.email, '') AS owner_email
		FROM tenant t
		JOIN tenant_company tc ON tc.tenant_id = t.id
		LEFT JOIN tenant_account ta ON ta.tenant_id = t.id AND ta.role = 'owner' AND ta.deleted_at IS NULL
		WHERE t.id = $1 AND t.deleted_at IS NULL
	`, id).Scan(&t.ID, &t.CompanyName, &t.SchemaName, &status, &t.CreatedAt, &t.OwnerEmail)
	if err == sql.ErrNoRows {
		return nil, &errs.Error{Code: errs.NotFound, Message: "tenant not found"}
	}
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "query failed"}
	}
	t.PlanTier = readPlanFromSchema(ctx, t.SchemaName)
	t.IsActive = status == "active"

	_ = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM tenant_account WHERE tenant_id = $1 AND deleted_at IS NULL`, id,
	).Scan(&t.AccountCount)
	_ = db.QueryRow(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM "%s".conversation WHERE deleted_at IS NULL`, t.SchemaName),
	).Scan(&t.ConversationCount)
	_ = db.QueryRow(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM "%s".message`, t.SchemaName),
	).Scan(&t.MessageCount)

	return &TenantDetailResponse{Tenant: t}, nil
}

// Impersonate creates an impersonation session for a tenant.
//
//encore:api auth method=POST path=/api/v1/admin/impersonate/:tenantId tag:super_admin
func Impersonate(ctx context.Context, tenantId string) (*ImpersonateResponse, error) {
	if err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}

	uid, _ := auth.UserID()
	adminID := string(uid)

	var sessionID string
	err := db.QueryRow(ctx, `
		INSERT INTO admin_session (admin_account_id, impersonated_tenant_id)
		VALUES ($1, $2)
		RETURNING id`,
		adminID, tenantId,
	).Scan(&sessionID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "session creation failed"}
	}

	details, _ := json.Marshal(map[string]string{"tenantId": tenantId, "action": "start"})
	_, err = db.Exec(ctx, `
		INSERT INTO impersonation_log (admin_session_id, action, details)
		VALUES ($1, 'impersonation.start', $2)`,
		sessionID, details)
	if err != nil {
		rlog.Error("failed to log impersonation start", "err", err)
	}

	expiresAt := time.Now().Add(2 * time.Hour)
	rlog.Info("impersonation started", "adminId", adminID, "tenantId", tenantId, "sessionId", sessionID)
	return &ImpersonateResponse{
		Token:     sessionID,
		ExpiresAt: expiresAt,
		TenantID:  tenantId,
	}, nil
}

// StopImpersonation ends the current impersonation session.
//
//encore:api auth method=POST path=/api/v1/admin/stop-impersonation tag:super_admin
func StopImpersonation(ctx context.Context) (*StopImpersonationResponse, error) {
	if err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}

	uid, _ := auth.UserID()
	adminID := string(uid)

	var sessionID string
	err := db.QueryRow(ctx, `
		UPDATE admin_session SET ended_at = NOW()
		WHERE admin_account_id = $1 AND ended_at IS NULL
		RETURNING id`,
		adminID,
	).Scan(&sessionID)
	if err == sql.ErrNoRows {
		return &StopImpersonationResponse{Message: "No active impersonation session"}, nil
	}
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to end session"}
	}

	details, _ := json.Marshal(map[string]string{"sessionId": sessionID, "action": "stop"})
	_, err = db.Exec(ctx, `
		INSERT INTO impersonation_log (admin_session_id, action, details)
		VALUES ($1, 'impersonation.stop', $2)`,
		sessionID, details)
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

func readPlanFromSchema(ctx context.Context, schema string) string {
	var plan string
	err := db.QueryRow(ctx, fmt.Sprintf(
		`SELECT COALESCE(plan_code,'starter') FROM "%s".subscription ORDER BY updated_at DESC LIMIT 1`, schema),
	).Scan(&plan)
	if err != nil || plan == "" {
		return "starter"
	}
	return plan
}
