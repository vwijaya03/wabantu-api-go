package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/billing"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/tenant"
)

var db = sqldb.Named("system")

// ---------- Types ----------

type Tenant struct {
	ID                 string     `json:"id"`
	CompanyName        string     `json:"companyName"`
	SchemaName         string     `json:"schemaName"`
	PlanTier           string     `json:"planTier"`
	IsActive           bool       `json:"isActive"`
	CreatedAt          time.Time  `json:"createdAt"`
	OwnerEmail         string     `json:"ownerEmail,omitempty"`
	SchemaMigratedAt   *time.Time `json:"schemaMigratedAt,omitempty"`
	SchemaPatchVersion int        `json:"schemaPatchVersion"`
	IsSchemaBehind     bool       `json:"isSchemaBehind"`
}

type TenantDetail struct {
	Tenant
	AccountCount      int `json:"accountCount"`
	ConversationCount int `json:"conversationCount"`
	MessageCount      int `json:"messageCount"`
}

type ListTenantsResponse struct {
	Tenants  []Tenant `json:"tenants"`
	Total    int      `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"pageSize"`
}

type ListTenantsParams struct {
	Q        string `query:"q"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type TenantDetailResponse struct {
	Tenant TenantDetail `json:"tenant"`
}

type ImpersonateResponse struct {
	OK     bool   `json:"ok"`
	Tenant Tenant `json:"tenant"`
}

type StopImpersonationResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type UpdateTenantPlanParams struct {
	PlanCode string `json:"planCode"`
}

type UpdateTenantPlanResponse struct {
	Tenant Tenant `json:"tenant"`
}

type DeleteTenantParams struct {
	ConfirmSchemaName string `query:"confirmSchemaName"`
}

type DeleteTenantResponse struct {
	OK         bool   `json:"ok"`
	TenantID   string `json:"tenantId"`
	SchemaName string `json:"schemaName"`
}

type MigrateTenantSchemasParams struct {
	TenantIDs []string `json:"tenantIds,omitempty"`
	Mode      string   `json:"mode,omitempty"` // "behind" | "selected" | ""
}

// ---------- Endpoints ----------

// ListTenants returns tenants with pagination and search.
//
//encore:api auth method=GET path=/api/v1/admin/tenants tag:super_admin
func ListTenants(ctx context.Context, p *ListTenantsParams) (*ListTenantsResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}

	if p.Page <= 0 {
		p.Page = 1
	}
	if p.PageSize <= 0 {
		p.PageSize = 10
	}
	if p.PageSize > 100 {
		p.PageSize = 100
	}

	conditions := []string{"t.deleted_at IS NULL"}
	args := []any{}
	if q := strings.TrimSpace(p.Q); q != "" {
		like := "%" + q + "%"
		conditions = append(conditions, `(t.name ILIKE $1 OR tc.schema_name ILIKE $1 OR COALESCE(owner.email,'') ILIKE $1)`)
		args = append(args, like)
	}
	where := strings.Join(conditions, " AND ")

	var total int
	countSQL := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM tenant t
		JOIN tenant_company tc ON tc.tenant_id = t.id
		LEFT JOIN LATERAL (
			SELECT email FROM tenant_account ta
			WHERE ta.tenant_id = t.id AND ta.role = 'owner' AND ta.deleted_at IS NULL
			ORDER BY ta.created_at ASC LIMIT 1
		) owner ON true
		WHERE %s`, where)
	if err := db.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "count tenants failed"}
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, p.PageSize, (p.Page-1)*p.PageSize)
	limitParam := len(queryArgs) - 1
	offsetParam := len(queryArgs)

	rows, err := db.Query(ctx, fmt.Sprintf(`
		SELECT t.id, t.name, tc.schema_name, t.status, t.created_at,
			COALESCE(owner.email, '') AS owner_email,
			tc.schema_migrated_at,
			COALESCE(tc.schema_patch_version, 0)
		FROM tenant t
		JOIN tenant_company tc ON tc.tenant_id = t.id
		LEFT JOIN LATERAL (
			SELECT email FROM tenant_account ta
			WHERE ta.tenant_id = t.id AND ta.role = 'owner' AND ta.deleted_at IS NULL
			ORDER BY ta.created_at ASC LIMIT 1
		) owner ON true
		WHERE %s
		ORDER BY t.created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, limitParam, offsetParam), queryArgs...)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "query failed"}
	}
	defer rows.Close()

	var tenants []Tenant
	for rows.Next() {
		var t Tenant
		var status string
		var migratedAt sql.NullTime
		if err := rows.Scan(&t.ID, &t.CompanyName, &t.SchemaName, &status, &t.CreatedAt, &t.OwnerEmail, &migratedAt, &t.SchemaPatchVersion); err != nil {
			return nil, &errs.Error{Code: errs.Internal, Message: "scan failed"}
		}
		if migratedAt.Valid {
			ts := migratedAt.Time
			t.SchemaMigratedAt = &ts
		}
		t.IsSchemaBehind = t.SchemaPatchVersion < tenant.CurrentSchemaPatchVersion
		t.PlanTier = readPlanFromSchema(ctx, t.SchemaName)
		t.IsActive = status == "active"
		tenants = append(tenants, t)
	}
	if err := rows.Err(); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "rows iteration failed"}
	}
	if tenants == nil {
		tenants = []Tenant{}
	}

	return &ListTenantsResponse{Tenants: tenants, Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

// GetTenant returns detailed info about a specific tenant.
//
//encore:api auth method=GET path=/api/v1/admin/tenant/:id tag:super_admin
func GetTenant(ctx context.Context, id string) (*TenantDetailResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}

	var t TenantDetail
	var status string
	var migratedAt sql.NullTime
	err := db.QueryRow(ctx, `
		SELECT t.id, t.name, tc.schema_name, t.status, t.created_at,
			COALESCE(ta.email, '') AS owner_email,
			tc.schema_migrated_at,
			COALESCE(tc.schema_patch_version, 0)
		FROM tenant t
		JOIN tenant_company tc ON tc.tenant_id = t.id
		LEFT JOIN tenant_account ta ON ta.tenant_id = t.id AND ta.role = 'owner' AND ta.deleted_at IS NULL
		WHERE t.id = $1 AND t.deleted_at IS NULL
	`, id).Scan(&t.ID, &t.CompanyName, &t.SchemaName, &status, &t.CreatedAt, &t.OwnerEmail, &migratedAt, &t.SchemaPatchVersion)
	if err == sql.ErrNoRows {
		return nil, &errs.Error{Code: errs.NotFound, Message: "tenant not found"}
	}
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "query failed"}
	}
	if migratedAt.Valid {
		ts := migratedAt.Time
		t.SchemaMigratedAt = &ts
	}
	t.IsSchemaBehind = t.SchemaPatchVersion < tenant.CurrentSchemaPatchVersion
	t.PlanTier = readPlanFromSchema(ctx, t.SchemaName)
	t.IsActive = status == "active"

	_ = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM tenant_account WHERE tenant_id = $1 AND deleted_at IS NULL`, id,
	).Scan(&t.AccountCount)
	if conn, err := tenant.TenantConn(ctx, t.SchemaName); err == nil {
		defer conn.Close()
		_ = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversation WHERE deleted_at IS NULL`).Scan(&t.ConversationCount)
		_ = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM message`).Scan(&t.MessageCount)
	} else {
		rlog.Warn("tenant detail counts failed to open tenant db", "tenantId", id, "schema", t.SchemaName, "err", err)
	}

	return &TenantDetailResponse{Tenant: t}, nil
}

// Impersonation switches the current session to view a tenant (Redis session update).
//
//encore:api auth method=POST path=/api/v1/admin/impersonate/:tenantId tag:super_admin
func Impersonate(ctx context.Context, tenantId string) (*ImpersonateResponse, error) {
	userData, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}

	if err := appauth.StartImpersonation(ctx, userData.AccountID, userData.SessionID, tenantId); err != nil {
		return nil, toEncoreErr(err)
	}

	uid, _ := auth.UserID()
	adminID := string(uid)

	var sessionID string
	err = db.QueryRow(ctx, `
		INSERT INTO admin_session (admin_account_id, impersonated_tenant_id)
		VALUES ($1, $2)
		RETURNING id`,
		adminID, tenantId,
	).Scan(&sessionID)
	if err != nil {
		rlog.Error("admin_session insert failed", "err", err)
	} else {
		details, _ := json.Marshal(map[string]string{"tenantId": tenantId, "action": "start"})
		_, _ = db.Exec(ctx, `
			INSERT INTO impersonation_log (admin_session_id, action, details)
			VALUES ($1, 'impersonation.start', $2)`,
			sessionID, details)
	}

	var t Tenant
	var status string
	_ = db.QueryRow(ctx, `
		SELECT t.id, t.name, tc.schema_name, t.status, t.created_at,
			COALESCE(ta.email, '') AS owner_email
		FROM tenant t
		JOIN tenant_company tc ON tc.tenant_id = t.id
		LEFT JOIN tenant_account ta ON ta.tenant_id = t.id AND ta.role = 'owner' AND ta.deleted_at IS NULL
		WHERE t.id = $1 AND t.deleted_at IS NULL`,
		tenantId,
	).Scan(&t.ID, &t.CompanyName, &t.SchemaName, &status, &t.CreatedAt, &t.OwnerEmail)
	t.IsActive = status == "active"

	rlog.Info("impersonation started", "adminId", adminID, "tenantId", tenantId)
	return &ImpersonateResponse{OK: true, Tenant: t}, nil
}

// StopImpersonation ends tenant view mode for the current session.
//
//encore:api auth method=POST path=/api/v1/admin/stop-impersonation tag:super_admin
func StopImpersonation(ctx context.Context) (*StopImpersonationResponse, error) {
	userData, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}

	if err := appauth.StopImpersonation(ctx, userData.AccountID, userData.SessionID); err != nil {
		return nil, toEncoreErr(err)
	}

	uid, _ := auth.UserID()
	adminID := string(uid)

	var sessionID string
	err = db.QueryRow(ctx, `
		UPDATE admin_session SET ended_at = NOW()
		WHERE admin_account_id = $1 AND ended_at IS NULL
		RETURNING id`,
		adminID,
	).Scan(&sessionID)
	if err == sql.ErrNoRows {
		return &StopImpersonationResponse{OK: true, Message: "Impersonation ended"}, nil
	}
	if err != nil {
		rlog.Error("admin_session end failed", "err", err)
	} else {
		details, _ := json.Marshal(map[string]string{"sessionId": sessionID, "action": "stop"})
		_, _ = db.Exec(ctx, `
			INSERT INTO impersonation_log (admin_session_id, action, details)
			VALUES ($1, 'impersonation.stop', $2)`,
			sessionID, details)
	}

	rlog.Info("impersonation stopped", "adminId", adminID)
	return &StopImpersonationResponse{OK: true, Message: "Impersonation ended"}, nil
}

// UpdateTenantPlan applies a package change directly from the platform console.
//
// This is an internal override for support/sales operations. Paid self-serve
// upgrades still go through billing.SelectPlan + payment webhook.
//
//encore:api auth method=PUT path=/api/v1/admin/tenant/:id/plan tag:super_admin
func UpdateTenantPlan(ctx context.Context, id string, p *UpdateTenantPlanParams) (*UpdateTenantPlanResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "plan tidak valid"}
	}
	planCode := strings.ToLower(strings.TrimSpace(p.PlanCode))
	plan, ok := billing.PlanCatalog[planCode]
	if !ok || planCode == "basic" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "plan tidak valid"}
	}

	t, err := loadTenant(ctx, id)
	if err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, t.SchemaName)
	if err != nil {
		rlog.Error("open tenant db for plan update failed", "tenantId", id, "schema", t.SchemaName, "err", err)
		return nil, &errs.Error{Code: errs.Internal, Message: "update plan failed"}
	}
	defer conn.Close()

	var subID string
	err = conn.QueryRowContext(ctx,
		`SELECT id FROM subscription WHERE status='active' ORDER BY updated_at DESC LIMIT 1`,
	).Scan(&subID)
	if err == sql.ErrNoRows {
		_, err = conn.ExecContext(ctx,
			`INSERT INTO subscription
			 (plan_code, plan_name, is_trial, trial_ends_at, status, provider, updated_at)
			 VALUES ($1,$2,false,NULL,'active','platform_admin',now())`,
			plan.Code, plan.Name)
	} else if err == nil {
		_, err = conn.ExecContext(ctx,
			`UPDATE subscription
			 SET plan_code=$1, plan_name=$2, is_trial=false, trial_ends_at=NULL,
			     status='active', provider='platform_admin', updated_at=now()
			 WHERE id=$3`,
			plan.Code, plan.Name, subID)
	}
	if err != nil {
		rlog.Error("tenant plan update query failed", "tenantId", id, "schema", t.SchemaName, "plan", plan.Code, "err", err)
		return nil, &errs.Error{Code: errs.Internal, Message: "update plan failed"}
	}

	t.PlanTier = plan.Code
	return &UpdateTenantPlanResponse{Tenant: *t}, nil
}

// DeleteTenant drops the tenant schema and soft-deletes system metadata.
//
// Destructive by design: callers must send the exact schema name to confirm.
//
//encore:api auth method=DELETE path=/api/v1/admin/tenant/:id tag:super_admin
func DeleteTenant(ctx context.Context, id string, p *DeleteTenantParams) (*DeleteTenantResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	t, err := loadTenant(ctx, id)
	if err != nil {
		return nil, err
	}
	if p == nil || strings.TrimSpace(p.ConfirmSchemaName) != t.SchemaName {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "konfirmasi schema tidak sesuai"}
	}
	if !strings.HasPrefix(t.SchemaName, "t_") || !validSchemaName(t.SchemaName) {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "schema tenant tidak aman untuk dihapus"}
	}

	if err := tenant.DropTenantSchema(ctx, t.SchemaName); err != nil {
		rlog.Error("tenant schema drop failed", "tenantId", id, "schema", t.SchemaName, "err", err)
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}

	tx, err := db.Stdlib().BeginTx(ctx, nil)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "begin delete failed"}
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx,
		`UPDATE tenant SET status='deleted', deleted_at=now(), updated_at=now()
		 WHERE id=$1 AND deleted_at IS NULL`, id); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "delete tenant failed"}
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE tenant_account SET deleted_at=now()
		 WHERE tenant_id=$1 AND deleted_at IS NULL`, id); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "delete tenant accounts failed"}
	}
	if err = tx.Commit(); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "commit delete failed"}
	}
	if _, err = db.Exec(ctx, `DELETE FROM payment_webhook_map WHERE tenant_schema=$1`, t.SchemaName); err != nil {
		rlog.Warn("delete payment webhook mappings failed", "tenantId", id, "err", err)
	}

	rlog.Warn("tenant schema wiped", "tenantId", id, "schema", t.SchemaName)
	return &DeleteTenantResponse{OK: true, TenantID: id, SchemaName: t.SchemaName}, nil
}

// MigrateTenantSchemas applies idempotent DDL patches (sync ≤3 tenants, else async job).
//
//encore:api auth method=POST path=/api/v1/admin/migrate-tenant-schemas tag:super_admin
func MigrateTenantSchemas(ctx context.Context, p *MigrateTenantSchemasParams) (*tenant.MigrateSchemasResponse, error) {
	user, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}
	var req *tenant.MigrateSchemasRequest
	if p != nil {
		req = &tenant.MigrateSchemasRequest{
			TenantIDs: p.TenantIDs,
			Mode:      p.Mode,
		}
	}
	return tenant.RunMigrateTenantSchemas(ctx, req, user.AccountID)
}

// GetMigrateTenantSchemasJob returns async migration job progress.
//
//encore:api auth method=GET path=/api/v1/admin/migrate-tenant-schemas/jobs/:jobId tag:super_admin
func GetMigrateTenantSchemasJob(ctx context.Context, jobId string) (*tenant.SchemaMigrationJobSummary, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	summary, err := tenant.GetSchemaMigrationJob(ctx, jobId)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, &errs.Error{Code: errs.NotFound, Message: err.Error()}
		}
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}
	return summary, nil
}

// ---------- Helpers ----------

func requireSuperAdmin(ctx context.Context) (*types.AuthUser, error) {
	userData, ok := auth.Data().(*types.AuthUser)
	if !ok || userData == nil {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "not authenticated"}
	}
	if userData.Role != "super_admin" {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "super admin access required"}
	}
	return userData, nil
}

func readPlanFromSchema(ctx context.Context, schema string) string {
	if !validSchemaName(schema) {
		return "starter"
	}
	conn, err := tenant.TenantConn(ctx, schema)
	if err != nil {
		rlog.Warn("read plan failed to open tenant db", "schema", schema, "err", err)
		return "starter"
	}
	defer conn.Close()

	var plan string
	err = conn.QueryRowContext(ctx,
		`SELECT COALESCE(plan_code,'starter') FROM subscription ORDER BY updated_at DESC LIMIT 1`,
	).Scan(&plan)
	if err != nil || plan == "" {
		return "starter"
	}
	return plan
}

func loadTenant(ctx context.Context, id string) (*Tenant, error) {
	var t Tenant
	var status string
	var migratedAt sql.NullTime
	err := db.QueryRow(ctx, `
		SELECT t.id, t.name, tc.schema_name, t.status, t.created_at,
			COALESCE(owner.email, '') AS owner_email,
			tc.schema_migrated_at,
			COALESCE(tc.schema_patch_version, 0)
		FROM tenant t
		JOIN tenant_company tc ON tc.tenant_id = t.id
		LEFT JOIN LATERAL (
			SELECT email FROM tenant_account ta
			WHERE ta.tenant_id = t.id AND ta.role = 'owner' AND ta.deleted_at IS NULL
			ORDER BY ta.created_at ASC LIMIT 1
		) owner ON true
		WHERE t.id = $1 AND t.deleted_at IS NULL`, id,
	).Scan(&t.ID, &t.CompanyName, &t.SchemaName, &status, &t.CreatedAt, &t.OwnerEmail, &migratedAt, &t.SchemaPatchVersion)
	if err == sql.ErrNoRows {
		return nil, &errs.Error{Code: errs.NotFound, Message: "tenant not found"}
	}
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "query tenant failed"}
	}
	if migratedAt.Valid {
		ts := migratedAt.Time
		t.SchemaMigratedAt = &ts
	}
	t.IsSchemaBehind = t.SchemaPatchVersion < tenant.CurrentSchemaPatchVersion
	t.PlanTier = readPlanFromSchema(ctx, t.SchemaName)
	t.IsActive = status == "active"
	return &t, nil
}

func validSchemaName(schema string) bool {
	if schema == "" || len(schema) > 63 {
		return false
	}
	for i, c := range schema {
		if i == 0 && !((c >= 'a' && c <= 'z') || c == '_') {
			return false
		}
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func toEncoreErr(err error) error {
	var e *errs.Error
	if errors.As(err, &e) {
		return e
	}
	return &errs.Error{Code: errs.Internal, Message: err.Error()}
}
