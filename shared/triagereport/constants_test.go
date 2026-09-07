package triagereport_test

import (
	"testing"

	"encore.app/wabantu/shared/triagereport"
)

func TestDailyLimitForRole(t *testing.T) {
	if triagereport.DailyLimitForRole(triagereport.ReporterRoleTenantUser) != 20 {
		t.Fatal("tenant limit want 20")
	}
	if triagereport.DailyLimitForRole(triagereport.ReporterRoleSuperAdmin) != 50 {
		t.Fatal("super_admin limit want 50")
	}
}

func TestReporterRoleFromAuth(t *testing.T) {
	if triagereport.ReporterRoleFromAuth("super_admin") != triagereport.ReporterRoleSuperAdmin {
		t.Fatal("expected super_admin role")
	}
	if triagereport.ReporterRoleFromAuth("owner") != triagereport.ReporterRoleTenantUser {
		t.Fatal("expected tenant_user role")
	}
}

func TestValidStatuses(t *testing.T) {
	for _, s := range []string{
		triagereport.StatusOpen,
		triagereport.StatusConfirmed,
		triagereport.StatusDismissed,
		triagereport.StatusResolved,
	} {
		if !triagereport.ValidStatuses[s] {
			t.Fatalf("status %s should be valid", s)
		}
	}
	if triagereport.ValidStatuses["invalid"] {
		t.Fatal("invalid status should be rejected")
	}
}

func TestValidCategories(t *testing.T) {
	for _, c := range []string{
		triagereport.CategoryWrongAnswer,
		triagereport.CategoryBug,
		triagereport.CategoryRude,
		triagereport.CategoryOffTopic,
		triagereport.CategoryOther,
	} {
		if !triagereport.ValidCategories[c] {
			t.Fatalf("category %s should be valid", c)
		}
	}
	if triagereport.ValidCategories["invalid"] {
		t.Fatal("invalid category should be rejected")
	}
}
