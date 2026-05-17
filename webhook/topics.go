package webhook

import (
	"context"

	"encore.dev/pubsub"
	"encore.dev/rlog"
)

type WebhookRetryRequest struct {
	TenantSchema string `json:"tenantSchema"`
	EventID      string `json:"eventId"`
	Attempt      int    `json:"attempt"`
}

type WebhookRetryDLQ struct {
	Original WebhookRetryRequest `json:"original"`
	Error    string              `json:"error"`
}

var WebhookRetryDLQTopic = pubsub.NewTopic[WebhookRetryDLQ]("webhook-retry-dlq", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

// WebhookRetryTopic queues failed outbound webhook deliveries for retry.
var WebhookRetryTopic = pubsub.NewTopic[WebhookRetryRequest]("webhook-retry", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

var _ = pubsub.NewSubscription(WebhookRetryTopic, "webhook-retryer", pubsub.SubscriptionConfig[WebhookRetryRequest]{
	Handler: handleWebhookRetry,
	RetryPolicy: &pubsub.RetryPolicy{
		MaxRetries: 5,
	},
})

func handleWebhookRetry(ctx context.Context, req WebhookRetryRequest) error {
	rlog.Info("webhook retry", "schema", req.TenantSchema, "eventId", req.EventID, "attempt", req.Attempt)
	return ProcessWebhookDelivery(ctx, req.TenantSchema, req.EventID)
}
