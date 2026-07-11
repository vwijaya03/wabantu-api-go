package ai

import (
	"context"
	"fmt"

	"encore.dev/pubsub"
	"encore.dev/rlog"
)

// ImageContextJob handles inbound images without caption when payment proof has no target order.
type ImageContextJob struct {
	TenantSchema     string `json:"tenantSchema"`
	TenantID         string `json:"tenantId"`
	ConversationID   string `json:"conversationId"`
	ContactID        string `json:"contactId"`
	MessageID        string `json:"messageId"`
	InboundMessageID string `json:"inboundMessageId"`
}

// ImageContextJobs processes vision catalog match (3c) or static fallback (3d).
var ImageContextJobs = pubsub.NewTopic[*ImageContextJob]("image-context-jobs", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

var _ = pubsub.NewSubscription(ImageContextJobs, "image-context-handler", pubsub.SubscriptionConfig[*ImageContextJob]{
	Handler:     handleImageContextJob,
	RetryPolicy: &pubsub.RetryPolicy{MaxRetries: 3},
})

// PublishImageContextJob enqueues image context processing for an uncaptioned inbound image.
func PublishImageContextJob(ctx context.Context, job *ImageContextJob) error {
	if job == nil || job.TenantSchema == "" || job.ConversationID == "" || job.MessageID == "" {
		return fmt.Errorf("invalid image context job")
	}
	if job.InboundMessageID == "" {
		job.InboundMessageID = job.MessageID
	}
	_, err := ImageContextJobs.Publish(ctx, job)
	return err
}

func handleImageContextJob(ctx context.Context, job *ImageContextJob) error {
	rlog.Info("processing image context job",
		"schema", job.TenantSchema,
		"conversationId", job.ConversationID,
		"messageId", job.MessageID,
	)
	if err := processImageContextJob(ctx, job); err != nil {
		rlog.Warn("image context job failed",
			"schema", job.TenantSchema,
			"messageId", job.MessageID,
			"err", err,
		)
		return err
	}
	return nil
}
