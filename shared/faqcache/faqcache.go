package faqcache

import (
	"context"
	"fmt"

	"encore.dev/pubsub"
	"encore.dev/rlog"
	"github.com/redis/go-redis/v9"
)

// InvalidateEvent requests FAQ-direct cache eviction for one tenant.
type InvalidateEvent struct {
	TenantID string `json:"tenant_id"`
}

// InvalidateTopic notifies workers to clear cached FAQ answers after KB changes.
var InvalidateTopic = pubsub.NewTopic[*InvalidateEvent]("faq-cache-invalidate", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

func keyPattern(tenantID string) string {
	return fmt.Sprintf("ai:faqcache:%s:*", tenantID)
}

// InvalidateTenant deletes all FAQ cache keys for tenantID from Redis.
func InvalidateTenant(ctx context.Context, rdb *redis.Client, tenantID string) {
	if rdb == nil || tenantID == "" {
		return
	}
	pattern := keyPattern(tenantID)
	var cursor uint64
	for {
		keys, next, err := rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			rlog.Warn("faq cache invalidate scan failed", "tenantId", tenantID, "err", err)
			return
		}
		if len(keys) > 0 {
			if err := rdb.Del(ctx, keys...).Err(); err != nil {
				rlog.Warn("faq cache invalidate delete failed", "tenantId", tenantID, "err", err)
				return
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
}
