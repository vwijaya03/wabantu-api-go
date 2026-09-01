package ai

import (
	"testing"

	"encore.app/wabantu/shared/retrieval"
)

func TestRetrieveKBRejectsInvalidTenantSchema(t *testing.T) {
	_, err := retrieval.Namespace(retrieval.TenantIdentity{
		TenantID:     "tenant-1",
		TenantSchema: "t_evil; DROP",
	})
	if err == nil {
		t.Fatal("expected namespace validation error before retrieval")
	}
}

func TestPerTenantEmbedQuotaRejectedMetric(t *testing.T) {
	before := retrieval.EmbedQuotaRejected()
	retrieval.RecordEmbedQuotaRejected()
	after := retrieval.EmbedQuotaRejected()
	if after != before+1 {
		t.Fatalf("quota rejected counter: before=%d after=%d", before, after)
	}
}
