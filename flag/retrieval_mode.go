package flag

import (
	"context"

	"encore.app/wabantu/shared/retrieval"
)

const (
	FlagRetrievalVector = "ai_retrieval_mode_vector"
	FlagRetrievalShadow = "ai_retrieval_mode_shadow"
)

// RetrievalMode resolves 3-state retrieval rollout for a tenant.
// Vector wins over shadow; default is disabled.
func RetrievalMode(ctx context.Context, tenantID string) retrieval.RetrievalMode {
	if IsEnabled(ctx, FlagRetrievalVector, tenantID) {
		return retrieval.ModeVector
	}
	if IsEnabled(ctx, FlagRetrievalShadow, tenantID) {
		return retrieval.ModeShadow
	}
	return retrieval.ModeDisabled
}
