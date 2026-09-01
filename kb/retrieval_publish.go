package kb

import "context"

// PublishRetrievalJob enqueues an indexing job (used by kb and business services).
func PublishRetrievalJob(ctx context.Context, job *RetrievalIndexJob) error {
	_, err := RetrievalIndexTopic.Publish(ctx, job)
	return err
}
