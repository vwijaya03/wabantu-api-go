package ai

import (
	"context"

	"encore.dev/pubsub"

	"encore.app/wabantu/shared/faqcache"
)

// InvalidateTenantFAQCache removes cached FAQ-direct answers for a tenant.
func InvalidateTenantFAQCache(ctx context.Context, tenantID string) {
	if svc == nil || tenantID == "" {
		return
	}
	svc.invalidateTenantFAQCache(ctx, tenantID)
}

func (s *AutoReplyService) invalidateTenantFAQCache(ctx context.Context, tenantID string) {
	faqcache.InvalidateTenant(ctx, s.rdb, tenantID)
}

var _ = pubsub.NewSubscription(faqcache.InvalidateTopic, "faq-cache-invalidator", pubsub.SubscriptionConfig[*faqcache.InvalidateEvent]{
	Handler: func(ctx context.Context, ev *faqcache.InvalidateEvent) error {
		if ev == nil || ev.TenantID == "" {
			return nil
		}
		InvalidateTenantFAQCache(ctx, ev.TenantID)
		return nil
	},
})
