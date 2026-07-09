package tenant_test

import (
	"context"
	"testing"

	"encore.app/wabantu/tenant"
)

func TestDropTenantSchema_InvalidName(t *testing.T) {
	err := tenant.DropTenantSchema(context.Background(), "not-valid")
	if err == nil {
		t.Fatal("expected error for invalid schema name")
	}
}
