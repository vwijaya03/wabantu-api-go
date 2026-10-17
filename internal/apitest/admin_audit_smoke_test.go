package apitest

import (
	"context"
	"testing"

	"encore.dev/beta/errs"

	"encore.app/wabantu/admin"
	"encore.app/wabantu/audit"
	"encore.app/wabantu/usage"
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

func TestAdminSmoke_LLMFirstUsage(t *testing.T) {
	RequireEncoreInfra(t)
	owner := BootstrapOwner(t)
	sa := BootstrapSuperAdmin(t)
	WithSuperAdminAuth(sa)
	ctx := context.Background()

	if err := usage.RecordLLMFirstTokens(ctx, owner.SchemaName, usage.LLMFirstTokens{
		Mode: "on", Pipeline: "llm_first", Model: "gemini-3.1-flash-lite", InputTokens: 1000, OutputTokens: 100,
	}); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	out, err := admin.GetLLMFirstUsage(ctx, &admin.LLMFirstUsageParams{TenantID: owner.TenantID})
	if err != nil {
		t.Fatalf("admin.GetLLMFirstUsage: %v", err)
	}
	AssertJSONFields(t, out, "tenantId", "days", "totals", "avgPerMessage", "byDay", "byModel", "idrPerUsd")
	if out.Days != 7 || out.Totals.Messages != 1 || out.Totals.LiveMessages != 1 || out.TenantID != owner.TenantID {
		t.Fatalf("report = %+v", out)
	}

	_, err = admin.GetLLMFirstUsage(ctx, &admin.LLMFirstUsageParams{TenantID: "7b1a6e0c-1d2e-4f3a-8b9c-0d1e2f3a4b5c"})
	if e, ok := err.(*errs.Error); !ok || e.Code != errs.NotFound {
		t.Fatalf("unknown tenant: %v", err)
	}
	_, err = admin.GetLLMFirstUsage(ctx, &admin.LLMFirstUsageParams{TenantID: "not-a-uuid"})
	if e, ok := err.(*errs.Error); !ok || e.Code != errs.InvalidArgument {
		t.Fatalf("bad tenant id: %v", err)
	}
}
