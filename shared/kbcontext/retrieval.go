package kbcontext

import (
	"context"

	"encore.app/wabantu/shared/retrieval"
)

// TenantRef identifies a tenant for retrieval.
type TenantRef struct {
	TenantID     string
	TenantSchema string
}

// RetrieveKB loads KB entries via the shared retrieval service.
func RetrieveKB(ctx context.Context, tenant TenantRef, query string, mode retrieval.RetrievalMode, lexicalRanker retrieval.LexicalRanker) (Bundle, error) {
	svc := retrieval.DefaultService()
	if svc == nil || mode == retrieval.ModeDisabled {
		return Bundle{Trace: FromRetrieveKBResult(query, mode, nil, DegradedNone)}, nil
	}
	res, err := svc.RetrieveKB(ctx, retrieval.RetrieveKBRequest{
		Tenant: retrieval.TenantIdentity{TenantID: tenant.TenantID, TenantSchema: tenant.TenantSchema},
		Query:  query,
		TopK:   20,
		Mode:   mode,
	}, lexicalRanker)
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{Trace: FromRetrieveKBResult(query, mode, res, DegradedNone)}, nil
}
