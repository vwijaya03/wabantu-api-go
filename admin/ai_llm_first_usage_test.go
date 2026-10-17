package admin

import (
	"context"
	"testing"

	encoreAuth "encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"

	"encore.app/wabantu/internal/adminauth"
	"encore.app/wabantu/shared/types"
)

func TestParseLLMFirstUsageDays(t *testing.T) {
	for raw, want := range map[string]int{"": 7, " ": 7, "1": 1, "7": 7, "90": 90} {
		got, err := parseLLMFirstUsageDays(raw)
		if err != nil || got != want {
			t.Errorf("%q = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"0", "-1", "91", "abc", "1.5", "9999999999999999999"} {
		_, err := parseLLMFirstUsageDays(raw)
		if e, ok := err.(*errs.Error); !ok || e.Code != errs.InvalidArgument {
			t.Errorf("%q: err = %v, want invalid_argument", raw, err)
		}
	}
}

func llmFirstUsageCode(t *testing.T, err error) errs.ErrCode {
	t.Helper()
	e, ok := err.(*errs.Error)
	if !ok {
		t.Fatalf("err = %v, want *errs.Error", err)
	}
	return e.Code
}

func TestGetLLMFirstUsageRequiresSuperAdmin(t *testing.T) {
	ctx := context.Background()
	p := &LLMFirstUsageParams{TenantID: "00000000-0000-0000-0000-000000000000"}

	// no auth data
	if _, err := GetLLMFirstUsage(ctx, p); llmFirstUsageCode(t, err) != errs.Unauthenticated {
		t.Fatalf("no auth: %v", err)
	}
	// tenant owner
	et.OverrideAuthInfo(encoreAuth.UID("u-owner"), &types.AuthUser{Role: "owner", SessionKind: adminauth.KindAdmin})
	if _, err := GetLLMFirstUsage(ctx, p); llmFirstUsageCode(t, err) != errs.PermissionDenied {
		t.Fatalf("owner: %v", err)
	}
	// super_admin role but impersonation (non-admin) session
	et.OverrideAuthInfo(encoreAuth.UID("u-imp"), &types.AuthUser{Role: "super_admin", SessionKind: adminauth.KindImpersonation})
	if _, err := GetLLMFirstUsage(ctx, p); llmFirstUsageCode(t, err) != errs.PermissionDenied {
		t.Fatalf("impersonation: %v", err)
	}
}

func TestGetLLMFirstUsageRejectsBadDaysBeforeTouchingTheDatabase(t *testing.T) {
	et.OverrideAuthInfo(encoreAuth.UID("u-sa"), &types.AuthUser{Role: "super_admin", SessionKind: adminauth.KindAdmin})
	_, err := GetLLMFirstUsage(context.Background(), &LLMFirstUsageParams{TenantID: "00000000-0000-0000-0000-000000000000", Days: "91"})
	if llmFirstUsageCode(t, err) != errs.InvalidArgument {
		t.Fatalf("err = %v", err)
	}
}
