package kbcontext

import (
	"context"

	appflag "encore.app/wabantu/flag"
	"encore.app/wabantu/shared/retrieval"
)

// retrievalProvider wires shared retrieval into the kbcontext Provider contract.
type retrievalProvider struct {
	lexical retrieval.LexicalRanker
}

// NewProvider returns a Provider that delegates to shared retrieval.
func NewProvider(lexical retrieval.LexicalRanker) Provider {
	return retrievalProvider{lexical: lexical}
}

func (p retrievalProvider) Retrieve(ctx context.Context, q Query) (Bundle, error) {
	mode := appflag.EffectiveRetrievalMode(ctx, q.TenantID, q.TenantSchema)
	return RetrieveKB(ctx, TenantRef{TenantID: q.TenantID, TenantSchema: q.TenantSchema}, q.Text, mode, p.lexical)
}
