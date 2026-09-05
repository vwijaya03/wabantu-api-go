package ai

import (
	"context"
	"fmt"

	"encore.dev/pubsub"
	"encore.dev/rlog"
)

// PaymentProofJob is published when an inbound image may be a transfer proof.
type PaymentProofJob struct {
	TenantSchema     string `json:"tenantSchema"`
	TenantID         string `json:"tenantId"`
	ConversationID   string `json:"conversationId"`
	ContactID        string `json:"contactId"`
	MessageID        string `json:"messageId"`
	InboundMessageID string `json:"inboundMessageId"`
}

// PaymentProofJobs processes buyer transfer proof screenshots asynchronously.
var PaymentProofJobs = pubsub.NewTopic[*PaymentProofJob]("payment-proof-jobs", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

var _ = pubsub.NewSubscription(PaymentProofJobs, "payment-proof-handler", pubsub.SubscriptionConfig[*PaymentProofJob]{
	Handler:     handlePaymentProofJob,
	RetryPolicy: &pubsub.RetryPolicy{MaxRetries: 3},
})

// PublishPaymentProofJob enqueues payment proof processing for an inbound image.
func PublishPaymentProofJob(ctx context.Context, job *PaymentProofJob) error {
	if job == nil || job.TenantSchema == "" || job.ConversationID == "" || job.MessageID == "" {
		return fmt.Errorf("invalid payment proof job")
	}
	if job.InboundMessageID == "" {
		job.InboundMessageID = job.MessageID
	}
	_, err := PaymentProofJobs.Publish(ctx, job)
	return err
}

func handlePaymentProofJob(ctx context.Context, job *PaymentProofJob) error {
	rlog.Info("processing payment proof job",
		"schema", job.TenantSchema,
		"conversationId", job.ConversationID,
		"messageId", job.MessageID,
	)
	inboundID := job.InboundMessageID
	if inboundID == "" {
		inboundID = job.MessageID
	}
	if !claimPaymentProofInbound(ctx, inboundID) {
		rlog.Info("payment proof job already processed", "inboundId", inboundID)
		return nil
	}
	if err := processPaymentProofJob(ctx, job); err != nil {
		releasePaymentProofInbound(ctx, inboundID)
		rlog.Warn("payment proof job failed",
			"schema", job.TenantSchema,
			"messageId", job.MessageID,
			"err", err,
		)
		return err
	}
	return nil
}
