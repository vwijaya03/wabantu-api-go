package apitest

import (
	"context"
	"testing"

	"encore.app/wabantu/admin"
	"encore.app/wabantu/audit"
)

func TestAdminSmoke_ListTenants(t *testing.T) {
	RequireEncoreInfra(t)
	_ = BootstrapOwner(t) // seed at least one tenant for the admin list query
	sa := BootstrapSuperAdmin(t)
	WithSuperAdminAuth(sa)

	ctx := context.Background()
	out, err := admin.ListTenants(ctx, &admin.ListTenantsParams{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("admin.ListTenants: %v", err)
	}
	AssertJSONFields(t, out, "tenants", "total", "page", "pageSize")
}

func TestAuditSmoke_ListAuditLogs(t *testing.T) {
	RequireEncoreInfra(t)
	sa := BootstrapSuperAdmin(t)
	WithSuperAdminAuth(sa)

	ctx := context.Background()
	out, err := audit.ListAuditLogs(ctx, &audit.ListAuditParams{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("audit.ListAuditLogs: %v", err)
	}
	AssertJSONFields(t, out, "logs", "total")
}
