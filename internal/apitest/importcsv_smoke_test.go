package apitest

import (
	"context"
	"testing"

	"encore.app/wabantu/importcsv"

	"encore.dev/beta/errs"
)

func TestImportCSVSsmoke_JobStatusNotFound(t *testing.T) {
	RequireEncoreInfra(t)
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)

	ctx := context.Background()
	_, err := importcsv.ImportJobStatus(ctx, "imp_nonexistent_job")
	if err == nil {
		t.Fatal("ImportJobStatus: expected error for missing job")
	}
	if e, ok := err.(*errs.Error); !ok || e.Code != errs.NotFound {
		t.Fatalf("ImportJobStatus: want NotFound, got %v", err)
	}
}
