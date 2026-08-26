package apitest

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/system"

	encoreAuth "encore.dev/beta/auth"
	"encore.dev/et"

	"golang.org/x/crypto/bcrypt"
)

const superAdminRole = "super_admin"

// SuperAdminFixture holds a platform super_admin account (no tenant_id).
type SuperAdminFixture struct {
	AccountID string
	Email     string
	Password  string
	Name      string
	Token     string
}

// AuthUser returns a platform AuthUser for et.OverrideAuthInfo.
func (f *SuperAdminFixture) AuthUser() *types.AuthUser {
	return &types.AuthUser{
		AccountID:         f.AccountID,
		Email:             f.Email,
		Name:              f.Name,
		Role:              superAdminRole,
		SessionID:         "apitest-super-admin",
		IsPlatformSession: true,
	}
}

// BootstrapSuperAdmin inserts a super_admin row in system DB (typed API via WithSuperAdminAuth).
func BootstrapSuperAdmin(t *testing.T) *SuperAdminFixture {
	t.Helper()
	RequireEncoreInfra(t)

	ctx := context.Background()
	slug := UniqueSlug(t)
	email := "super_" + slug + "@apitest.local"
	name := "Apitest Super Admin"
	password := defaultSmokePassword
	emailHash := hashEmail(email)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		t.Fatalf("password hash: %v", err)
	}

	accountID := uuid.New().String()
	if _, err := system.DB.Exec(ctx,
		`INSERT INTO tenant_account (id, email, email_hash, password_hash, name, tenant_id, role)
		 VALUES ($1::uuid, $2, $3, $4, $5, NULL, $6)`,
		accountID, email, emailHash, string(passwordHash), name, superAdminRole,
	); err != nil {
		t.Fatalf("insert super_admin: %v", err)
	}

	return &SuperAdminFixture{
		AccountID: accountID,
		Email:     email,
		Password:  password,
		Name:      name,
	}
}

// WithSuperAdminAuth sets encore auth context for super_admin typed API handlers.
func WithSuperAdminAuth(fx *SuperAdminFixture) {
	et.OverrideAuthInfo(encoreAuth.UID(fx.AccountID), fx.AuthUser())
}
