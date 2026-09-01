package flag

import (
	"context"

	"encore.app/wabantu/kb"
	"encore.app/wabantu/shared/retrieval"
)

const (
	FlagRetrievalVector = "ai_retrieval_mode_vector"
	FlagRetrievalShadow = "ai_retrieval_mode_shadow"
)

// RetrievalMode resolves 3-state retrieval rollout for a tenant.
// Vector wins over shadow; default is disabled.
func RetrievalMode(ctx context.Context, tenantID string) retrieval.RetrievalMode {
	return EffectiveRetrievalMode(ctx, tenantID, "")
}

// EffectiveRetrievalMode applies indexing gate: vector mode downgrades to shadow when <90% indexed.
func EffectiveRetrievalMode(ctx context.Context, tenantID, tenantSchema string) retrieval.RetrievalMode {
	if IsEnabled(ctx, FlagRetrievalVector, tenantID) {
		if tenantSchema != "" {
			if pct, err := kbIndexedPercent(ctx, tenantSchema); err == nil && pct < retrieval.MinIndexedPercentVector {
				return retrieval.ModeShadow
			}
		}
		return retrieval.ModeVector
	}
	if IsEnabled(ctx, FlagRetrievalShadow, tenantID) {
		return retrieval.ModeShadow
	}
	return retrieval.ModeDisabled
}

func kbIndexedPercent(ctx context.Context, tenantSchema string) (int, error) {
	return kb.IndexedPercentComplete(ctx, tenantSchema)
}
