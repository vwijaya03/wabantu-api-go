package ai

import (
	"context"

	"encore.dev/pubsub"
	"encore.dev/rlog"
)

type AutoReplyRequest struct {
	TenantSchema   string `json:"tenantSchema"`
	ConversationID string `json:"conversationId"`
	ContactJID     string `json:"contactJid"`
	MessageBody    string `json:"messageBody"`
	MessageType    string `json:"messageType"`
}

type AutoReplyDLQ struct {
	Original AutoReplyRequest `json:"original"`
	Error    string           `json:"error"`
}

var AutoReplyDLQTopic = pubsub.NewTopic[AutoReplyDLQ]("ai-reply-dlq", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

// AutoReplyTopic queues AI auto-reply jobs (replaces Node.js BullMQ).
var AutoReplyTopic = pubsub.NewTopic[AutoReplyRequest]("ai-reply", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

var _ = pubsub.NewSubscription(AutoReplyTopic, "ai-replier", pubsub.SubscriptionConfig[AutoReplyRequest]{
	Handler: handleAutoReply,
	RetryPolicy: &pubsub.RetryPolicy{
		MaxRetries: 5,
	},
})

func handleAutoReply(ctx context.Context, req AutoReplyRequest) error {
	rlog.Info("ai auto-reply triggered",
		"schema", req.TenantSchema,
		"conversationId", req.ConversationID,
		"contactJid", req.ContactJID,
	)
	// Placeholder: load business profile, KB, history, call Anthropic, send WA reply
	return nil
}
