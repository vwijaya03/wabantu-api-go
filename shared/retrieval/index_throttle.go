package retrieval

import "context"

const (
	IndexLaneLive     = "live"
	IndexLaneBackfill = "backfill"
)

var (
	liveIndexBudget     = NewBudget(4)
	backfillIndexBudget = NewBudget(2)
)

// WithIndexingLane limits concurrent OpenAI/Pinecone calls for indexing workers.
// Live CRUD indexing gets a higher budget than bulk backfill.
func WithIndexingLane(ctx context.Context, lane string, fn func() error) error {
	if lane == IndexLaneBackfill {
		return WithBudget(ctx, backfillIndexBudget, fn)
	}
	return WithBudget(ctx, liveIndexBudget, fn)
}
