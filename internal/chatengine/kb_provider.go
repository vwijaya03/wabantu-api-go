package chatengine

import (
	"context"

	appflag "encore.app/wabantu/flag"
	"encore.app/wabantu/shared/kbcontext"
	"encore.app/wabantu/shared/retrieval"
)

// KBProvider wires shared retrieval for web chat (lives here to keep kbcontext free of flag/kb imports).
type KBProvider struct {
	Lexical retrieval.LexicalRanker
}

func (p KBProvider) Retrieve(ctx context.Context, q kbcontext.Query) (kbcontext.Bundle, error) {
	mode := appflag.EffectiveRetrievalMode(ctx, q.TenantID, q.TenantSchema)
	return kbcontext.RetrieveKB(ctx, kbcontext.TenantRef{
		TenantID: q.TenantID, TenantSchema: q.TenantSchema,
	}, q.Text, mode, p.Lexical)
}
