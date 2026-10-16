package ai

import (
	"context"

	"encore.dev/pubsub"

	"encore.app/wabantu/kb"
)

// Pub/Sub push handler lives in ai (same as ai-auto-reply) so Cloud Run JWT validation matches other workers.
var _ = pubsub.NewSubscription(kb.RetrievalIndexTopic, "retrieval-index-worker", pubsub.SubscriptionConfig[*kb.RetrievalIndexJob]{
	Handler: func(ctx context.Context, job *kb.RetrievalIndexJob) error {
		return kb.HandleRetrievalIndexJob(ctx, job)
	},
	RetryPolicy: &pubsub.RetryPolicy{MaxRetries: 5},
})
