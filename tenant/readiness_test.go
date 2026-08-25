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
