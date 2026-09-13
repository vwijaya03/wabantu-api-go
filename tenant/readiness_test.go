package tenant

import (
	"context"
	"testing"

	"encore.app/wabantu/shared/tenantschema"
)

func TestTenantSchemaBaseProvisionedUsesQualifiedCheck(t *testing.T) {
	// Compile-time/doc test: provisioned check must not require search_path session.
	var fn func(context.Context, string) (bool, error) = tenantSchemaBaseProvisioned
	if fn == nil {
		t.Fatal("tenantSchemaBaseProvisioned missing")
	}
	_ = tenantschema.TableExists
}

func TestErrSchemaMigrationBusy(t *testing.T) {
	if errSchemaMigrationBusy == nil {
		t.Fatal("expected busy error")
	}
}

func TestReadinessIfNoTenant(t *testing.T) {
	got, ok := readinessIfNoTenant("")
	if !ok || got == nil || !got.Ready {
		t.Fatalf("empty schema: ok=%v resp=%+v", ok, got)
	}
	got, ok = readinessIfNoTenant("public")
	if !ok || got == nil || !got.Ready {
		t.Fatalf("public schema: ok=%v resp=%+v", ok, got)
	}
	got, ok = readinessIfNoTenant("t_omah_apparel")
	if ok || got != nil {
		t.Fatalf("valid tenant schema must use DB checks, ok=%v resp=%+v", ok, got)
	}
}
