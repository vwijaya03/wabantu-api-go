package ai

import (
	"context"

	"encore.app/wabantu/tenant"
)

// ProcessAutoReplyJob runs the AI auto-reply pipeline for an inbound message.
func ProcessAutoReplyJob(ctx context.Context, tenantID, tenantSchema, conversationID, inboundMessageID string) (bool, error) {
	if tenantID == "" && tenantSchema != "" {
		if id, err := tenant.TenantIDBySchema(ctx, tenantSchema); err == nil {
			tenantID = id
		}
	}
	return svc.ProcessAutoReply(ctx, AiReplyJobPayload{
		TenantID:         tenantID,
		TenantSchema:     tenantSchema,
		ConversationID:   conversationID,
		InboundMessageID: inboundMessageID,
	})
}

// FallbackAutoReplyJob sends the static fallback message after AI retries are exhausted.
func FallbackAutoReplyJob(ctx context.Context, tenantID, tenantSchema, conversationID, inboundMessageID string) error {
	if tenantID == "" && tenantSchema != "" {
		if id, err := tenant.TenantIDBySchema(ctx, tenantSchema); err == nil {
			tenantID = id
		}
	}
	return svc.FallbackAutoReply(ctx, AiReplyJobPayload{
		TenantID:         tenantID,
		TenantSchema:     tenantSchema,
		ConversationID:   conversationID,
		InboundMessageID: inboundMessageID,
	})
}
