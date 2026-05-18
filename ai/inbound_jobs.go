package ai

import (
	"context"
	"fmt"
	"time"

	"encore.dev/pubsub"
	"encore.dev/rlog"

	"encore.app/wabantu/tenant"
)

const (
	maxInboundAIAttempts = 4
	aiAttemptKeyPrefix   = "ai:job:attempt:"
	aiAttemptTTL         = 24 * time.Hour
)

// InboundAIJob is published when an inbound WhatsApp message needs AI processing.
type InboundAIJob struct {
	TenantSchema     string `json:"tenantSchema"`
	ConversationID   string `json:"conversationId"`
	InboundMessageID string `json:"inboundMessageId"`
	InboundType      string `json:"inboundType"`
}

// InboundAIJobs is the primary AI auto-reply queue (replaces NestJS BullMQ + ai-worker).
var InboundAIJobs = pubsub.NewTopic[*InboundAIJob]("ai-jobs", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

var _ = pubsub.NewSubscription(InboundAIJobs, "ai-auto-reply", pubsub.SubscriptionConfig[*InboundAIJob]{
	Handler:     handleInboundAI,
	RetryPolicy: &pubsub.RetryPolicy{MaxRetries: 3},
})

// PublishInboundJob enqueues AI processing for an inbound message.
func PublishInboundJob(ctx context.Context, job *InboundAIJob) error {
	if job == nil || job.TenantSchema == "" || job.ConversationID == "" || job.InboundMessageID == "" {
		return fmt.Errorf("invalid inbound AI job")
	}
	_, err := InboundAIJobs.Publish(ctx, job)
	return err
}

func handleInboundAI(ctx context.Context, job *InboundAIJob) error {
	tenantID, err := tenant.TenantIDBySchema(ctx, job.TenantSchema)
	if err != nil {
		rlog.Warn("inbound AI: tenant lookup failed", "schema", job.TenantSchema, "err", err)
		tenantID = ""
	}

	attempt := incrementAIAttempt(ctx, job.InboundMessageID)
	rlog.Info("processing inbound AI job",
		"schema", job.TenantSchema,
		"conversationId", job.ConversationID,
		"inboundId", job.InboundMessageID,
		"attempt", attempt,
	)

	sent, procErr := ProcessAutoReplyJob(ctx, tenantID, job.TenantSchema, job.ConversationID, job.InboundMessageID)
	if procErr == nil {
		clearAIAttempt(ctx, job.InboundMessageID)
		rlog.Info("inbound AI job done",
			"conversationId", job.ConversationID,
			"inboundId", job.InboundMessageID,
			"sent", sent,
		)
		return nil
	}

	rlog.Warn("inbound AI job failed",
		"conversationId", job.ConversationID,
		"inboundId", job.InboundMessageID,
		"attempt", attempt,
		"err", procErr,
	)

	if attempt >= maxInboundAIAttempts {
		clearAIAttempt(ctx, job.InboundMessageID)
		if fbErr := FallbackAutoReplyJob(ctx, tenantID, job.TenantSchema, job.ConversationID, job.InboundMessageID); fbErr != nil {
			rlog.Warn("AI fallback failed",
				"conversationId", job.ConversationID,
				"inboundId", job.InboundMessageID,
				"err", fbErr,
			)
		} else {
			rlog.Warn("AI fallback sent after max retries",
				"conversationId", job.ConversationID,
				"inboundId", job.InboundMessageID,
			)
		}
		return nil
	}

	return procErr
}

func incrementAIAttempt(ctx context.Context, inboundMessageID string) int {
	rdb := svc.rdb
	if rdb == nil {
		return 1
	}
	key := aiAttemptKeyPrefix + inboundMessageID
	n, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		return 1
	}
	_ = rdb.Expire(ctx, key, aiAttemptTTL).Err()
	return int(n)
}

func clearAIAttempt(ctx context.Context, inboundMessageID string) {
	if svc.rdb == nil {
		return
	}
	_ = svc.rdb.Del(ctx, aiAttemptKeyPrefix+inboundMessageID).Err()
}
