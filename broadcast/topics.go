package broadcast

import "encore.dev/pubsub"

type BroadcastSendRequest struct {
	TenantSchema string   `json:"tenantSchema"`
	BroadcastID  string   `json:"broadcastId"`
	TemplateID   string   `json:"templateId"`
	Recipients   []string `json:"recipients"`
}

// BroadcastSendTopic queues broadcast sending jobs.
// Handler will be implemented when broadcast feature is built.
var BroadcastSendTopic = pubsub.NewTopic[BroadcastSendRequest]("broadcast-send", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})
