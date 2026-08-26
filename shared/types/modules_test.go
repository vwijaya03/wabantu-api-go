package types

import (
	"testing"

	"encore.dev/beta/errs"
)

func TestValidTenantModule(t *testing.T) {
	if !ValidTenantModule("finance") {
		t.Fatal("expected finance valid")
	}
	if ValidTenantModule("platform") {
		t.Fatal("platform is not a tenant nav module")
	}
}

func TestRequireModuleImpersonatingFull(t *testing.T) {
	u := &AuthUser{Impersonating: true, ImpersonationModules: []string{}}
	if err := u.RequireModule("finance"); err != nil {
		t.Fatalf("full grant should allow any module: %v", err)
	}
}

func TestRequireModuleImpersonatingLimited(t *testing.T) {
	u := &AuthUser{
		Impersonating:        true,
		ImpersonationScope:   "limited",
		ImpersonationModules: []string{"finance"},
	}
	if err := u.RequireModule("finance"); err != nil {
		t.Fatalf("expected finance allowed: %v", err)
	}
	err := u.RequireModule("sales")
	if err == nil {
		t.Fatal("expected sales denied")
	}
	e, ok := err.(*errs.Error)
	if !ok || e.Code != errs.PermissionDenied {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

func TestRequireModuleNotImpersonating(t *testing.T) {
	u := &AuthUser{Role: "owner"}
	if err := u.RequireModule("sales"); err != nil {
		t.Fatalf("non-impersonating should skip gate: %v", err)
	}
}
