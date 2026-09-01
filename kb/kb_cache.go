package kb

import (
	"context"

	"encore.app/wabantu/shared/faqcache"
)

func invalidateFAQCacheAfterKBChange(ctx context.Context, tenantID string) {
	if tenantID == "" {
		return
	}
	_, _ = faqcache.InvalidateTopic.Publish(ctx, &faqcache.InvalidateEvent{TenantID: tenantID})
}
