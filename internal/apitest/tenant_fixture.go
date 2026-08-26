package apitest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"encore.app/wabantu/auth"
	"encore.app/wabantu/branch"
	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/pricing"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/system"
	"encore.app/wabantu/tenant"

	"golang.org/x/crypto/bcrypt"
)

const defaultSmokePassword = "SmokeTest!Pass123"

// TenantFixture holds a provisioned tenant schema and owner account in the system DB.
type TenantFixture struct {
	TenantID   string
	TenantSlug string
	TenantName string
	SchemaName string
	CompanyID  string
	AccountID  string
	Email      string
	Password   string
	Name       string
	Role       string
	Token      string
}

// AuthUser returns an owner AuthUser for et.OverrideAuthInfo / encore API smoke calls.
func (f *TenantFixture) AuthUser() *types.AuthUser {
	return &types.AuthUser{
		AccountID:        f.AccountID,
		TenantID:         f.TenantID,
		TenantSchema:     f.SchemaName,
		Email:            f.Email,
		Name:             f.Name,
		Role:             f.Role,
		SessionID:        "apitest-smoke",
		HomeTenantID:     f.TenantID,
		HomeTenantSchema: f.SchemaName,
	}
}

// OwnerFixture is an alias for tenant + JWT used by R3 service smoke tests.
type OwnerFixture = TenantFixture

func hashEmail(email string) string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:])
}

// BootstrapTenant provisions system rows, tenant.RunTenantDDL, and default branch seed.
func BootstrapTenant(t *testing.T) *TenantFixture {
	return bootstrapTenant(t, defaultSmokePassword, true)
}

// BootstrapOwner provisions a tenant for typed API smokes (et.OverrideAuthInfo; no Redis/JWT).
func BootstrapOwner(t *testing.T) *TenantFixture {
	RequireEncoreInfra(t)
	return bootstrapTenant(t, defaultSmokePassword, false)
}

// BootstrapOwnerWithToken provisions tenant + JWT (requires Redis; for auth HTTP smokes).
func BootstrapOwnerWithToken(t *testing.T) *TenantFixture {
	RequireRedis(t)
	return bootstrapTenant(t, defaultSmokePassword, true)
}

func bootstrapTenant(t *testing.T, password string, issueToken bool) *TenantFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("apitest smoke requires database (skip with -short)")
	}

	ctx := context.Background()
	slug := UniqueSlug(t)
	schemaName := "t_" + slug
	tenantName := "Apitest " + slug
	email := slug + "@apitest.local"
	name := "Apitest Owner"
	role := "owner"
	emailHash := hashEmail(email)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		t.Fatalf("password hash: %v", err)
	}

	tx, err := system.DB.Stdlib().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()

	var tenantID string
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO tenant (slug, name, status) VALUES ($1, $2, 'active')
		 RETURNING id::text`,
		slug, tenantName,
	).Scan(&tenantID); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	var companyID string
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO tenant_company (tenant_id, schema_name) VALUES ($1::uuid, $2) RETURNING id::text`,
		tenantID, schemaName,
	).Scan(&companyID); err != nil {
		t.Fatalf("insert tenant_company: %v", err)
	}

	accountID := uuid.New().String()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tenant_account (id, email, email_hash, password_hash, name, tenant_id, role)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7)`,
		accountID, email, emailHash, string(passwordHash), name, tenantID, role,
	); err != nil {
		t.Fatalf("insert tenant_account: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if err := tenant.RunTenantDDL(ctx, schemaName); err != nil {
		t.Fatalf("RunTenantDDL(%s): %v", schemaName, err)
	}
	if err := tenant.RunEventsSchemaPatches(ctx, schemaName); err != nil {
		t.Fatalf("RunEventsSchemaPatches(%s): %v", schemaName, err)
	}
	if err := seedDefaultPriceType(ctx, schemaName); err != nil {
		t.Fatalf("seed default price type: %v", err)
	}

	sch := appdb.SchemaSQL{Schema: schemaName}
	if _, err := tenant.DataDB.Stdlib().ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (business_name, tone, ai_enabled)
		 VALUES ($1, 'friendly', true) ON CONFLICT DO NOTHING`, sch.T("business_profile")),
		tenantName,
	); err != nil {
		t.Fatalf("seed business_profile: %v", err)
	}
	if err := branch.EnsureDefaultBranch(ctx, schemaName); err != nil {
		t.Fatalf("EnsureDefaultBranch: %v", err)
	}
	if err := tenant.RecordNewTenantSchemaVersion(ctx, tenantID); err != nil {
		t.Fatalf("RecordNewTenantSchemaVersion: %v", err)
	}

	fix := &TenantFixture{
		TenantID:   tenantID,
		TenantSlug: slug,
		TenantName: tenantName,
		SchemaName: schemaName,
		CompanyID:  companyID,
		AccountID:  accountID,
		Email:      email,
		Password:   password,
		Name:       name,
		Role:       role,
	}

	if issueToken {
		token, err := auth.IssueTestAccessToken(ctx, auth.SessionData{
			AccountID:    accountID,
			TenantID:     tenantID,
			TenantSchema: schemaName,
			Role:         role,
			Email:        email,
			Name:         name,
		})
		if err != nil {
			t.Fatalf("issue access token: %v", err)
		}
		fix.Token = token
	}

	return fix
}

// LoginOwner verifies credentials and issues a fresh JWT (mirrors auth/login without raw HTTP).
func LoginOwner(t *testing.T, email, password string) *TenantFixture {
	t.Helper()
	RequireRedis(t)

	ctx := context.Background()
	emailHash := hashEmail(email)

	var accountID, storedHash, dbEmail string
	var accountName sql.NullString
	var accountRole string
	var accountTenantID sql.NullString
	err := system.DB.QueryRow(ctx,
		`SELECT id::text, password_hash, email, name, role, tenant_id
		 FROM tenant_account
		 WHERE email_hash = $1 AND deleted_at IS NULL`,
		emailHash,
	).Scan(&accountID, &storedHash, &dbEmail, &accountName, &accountRole, &accountTenantID)
	if err != nil {
		t.Fatalf("login lookup: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)); err != nil {
		t.Fatalf("invalid credentials")
	}
	if !accountTenantID.Valid || accountTenantID.String == "" {
		t.Fatal("account not linked to tenant")
	}

	var tenantSlug, tenantName string
	if err := system.DB.QueryRow(ctx,
		`SELECT slug, name FROM tenant WHERE id = $1::uuid AND deleted_at IS NULL`,
		accountTenantID.String,
	).Scan(&tenantSlug, &tenantName); err != nil {
		t.Fatalf("tenant lookup: %v", err)
	}

	var schemaName string
	if err := system.DB.QueryRow(ctx,
		`SELECT schema_name FROM tenant_company WHERE tenant_id = $1::uuid`,
		accountTenantID.String,
	).Scan(&schemaName); err != nil {
		t.Fatalf("company lookup: %v", err)
	}

	name := ""
	if accountName.Valid {
		name = accountName.String
	}

	token, err := auth.IssueTestAccessToken(ctx, auth.SessionData{
		AccountID:    accountID,
		TenantID:     accountTenantID.String,
		TenantSchema: schemaName,
		Role:         accountRole,
		Email:        dbEmail,
		Name:         name,
	})
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	return &TenantFixture{
		TenantID:   accountTenantID.String,
		TenantSlug: tenantSlug,
		TenantName: tenantName,
		SchemaName: schemaName,
		AccountID:  accountID,
		Email:      dbEmail,
		Password:   password,
		Name:       name,
		Role:       accountRole,
		Token:      token,
	}
}

func seedDefaultPriceType(ctx context.Context, schemaName string) error {
	pool := tenant.DataDB.Stdlib()
	if err := pricing.EnsureSchema(ctx, pool, schemaName); err != nil {
		return err
	}
	ts := appdb.OpenTenantScope(pool, schemaName)
	_, err := ts.ExecContext(ctx, `
		INSERT INTO business_price_type (code, label, display_order, is_default, is_system, is_active)
		SELECT 'umum', 'Harga umum', 1, true, true, true
		WHERE NOT EXISTS (
			SELECT 1 FROM business_price_type WHERE code = 'umum' AND deleted_at IS NULL
		)`)
	return err
}
