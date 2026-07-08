package tenant_test

import (
	"context"
	"testing"

	"encore.app/wabantu/tenant"
)

func TestEnsureKnowledgeBaseSchema_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("requires tenant database")
	}
	ctx := context.Background()
	schema := "t_test_kb_patch"
	if err := tenant.RunTenantDDL(ctx, schema); err != nil {
		t.Fatalf("RunTenantDDL: %v", err)
	}
	if err := tenant.EnsureKnowledgeBaseSchema(ctx, schema); err != nil {
		t.Fatalf("first EnsureKnowledgeBaseSchema: %v", err)
	}
	if err := tenant.EnsureKnowledgeBaseSchema(ctx, schema); err != nil {
		t.Fatalf("second EnsureKnowledgeBaseSchema: %v", err)
	}
}
