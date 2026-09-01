package ai

import (
	"context"
	"fmt"
	"time"

	"encore.dev/rlog"
)

// tenantEmbedQuotaPerHour limits OpenAI embedding calls per tenant (cost DoS protection).
const tenantEmbedQuotaPerHour = 500

func embedQuotaRedisKey(tenantID string, at time.Time) string {
	return fmt.Sprintf("retrieval:embedquota:%s:%s", tenantID, at.UTC().Format("2006010215"))
}

// checkTenantEmbedQuota returns true when the tenant may call the embedding API.
// Fail-closed when Redis is unavailable to protect shared OpenAI quota.
func (s *AutoReplyService) checkTenantEmbedQuota(ctx context.Context, tenantID string) bool {
	if s == nil || s.rdb == nil {
		return true
	}
	key := embedQuotaRedisKey(tenantID, time.Now())
	n, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		rlog.Error("embed quota redis incr failed", "tenant", tenantID, "err", err)
		return false
	}
	if n == 1 {
		_ = s.rdb.Expire(ctx, key, time.Hour).Err()
	}
	return n <= tenantEmbedQuotaPerHour
}
