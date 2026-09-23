package kbcontext

import (
	"context"
	"testing"

	"encore.app/wabantu/shared/retrieval"
)

func TestRetrieveCatalogDisabledMode(t *testing.T) {
	res, err := RetrieveCatalog(context.Background(), TenantRef{TenantID: "t1", TenantSchema: "t_acme"}, "durian", retrieval.ModeDisabled, 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.Trace.Hash == "" {
		t.Fatal("expected trace hash")
	}
}
