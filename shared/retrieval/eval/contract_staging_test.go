//go:build staging

package eval_test

import (
	"context"
	"os"
	"testing"

	"encore.app/wabantu/shared/retrieval"
)

// Contract test against real OpenAI + Pinecone (staging/nightly only).
func TestProviderRoundtrip(t *testing.T) {
	if os.Getenv("RUN_RAG_CONTRACT") != "1" {
		t.Skip("set RUN_RAG_CONTRACT=1 to run staging contract test")
	}
	svc := retrieval.NewProductionService()
	if svc == nil {
		t.Fatal("production retrieval service not configured")
	}
	tenant := retrieval.TenantIdentity{TenantSchema: "t_contract"}
	ctx := context.Background()
	in := retrieval.KBIndexInput{
		Tenant: tenant, EntryID: "contract-test", Question: "test?", Answer: "ok", Version: 1,
	}
	if err := retrieval.IndexKBEntry(ctx, svc, in); err != nil {
		t.Fatal(err)
	}
	res, err := svc.RetrieveKB(ctx, retrieval.RetrieveKBRequest{
		Tenant: tenant, Query: "test", TopK: 3, Mode: retrieval.ModeVector,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) == 0 {
		t.Fatal("expected vector hit")
	}
	_ = retrieval.DeleteKBEntryVectors(ctx, svc, tenant, "contract-test", 1)
}
