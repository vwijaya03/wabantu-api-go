package chatwidget

import (
	"context"
	"fmt"
	"strings"
	"time"

	"encore.app/wabantu/auth"
	appflag "encore.app/wabantu/flag"
	"encore.app/wabantu/shared/kbcontext"
	"encore.app/wabantu/usage"
)

const (
	maxPublicMessageLen = 1000
	rateLimitPerMinute  = 30
	flagChatKillSwitch  = "chat_widget_kill_switch"
)

// abuseGate returns block=true for hard rejects; degraded for soft quota fallback (FAQ-only).
func abuseGate(ctx context.Context, tenantSchema, tenantID, clientIP, body string) (block bool, degraded kbcontext.DegradedMode, reason string) {
	if appflag.IsEnabled(ctx, flagChatKillSwitch, tenantID) {
		return true, kbcontext.DegradedKillSwitch, "kill_switch"
	}
	if len(strings.TrimSpace(body)) > maxPublicMessageLen {
		return true, kbcontext.DegradedNone, "message_too_long"
	}
	if clientIP != "" {
		key := fmt.Sprintf("chatwidget:rate:%s:%s", tenantID, clientIP)
		rdb := auth.RedisClient()
		if rdb != nil {
			n, err := rdb.Incr(ctx, key).Result()
			if err == nil {
				if n == 1 {
					_ = rdb.Expire(ctx, key, time.Minute).Err()
				}
				if n > rateLimitPerMinute {
					return true, kbcontext.DegradedNone, "rate_limited"
				}
			}
		}
	}
	ok, why := usage.CheckAICostLimit(ctx, tenantSchema, tenantID)
	if !ok {
		return false, kbcontext.DegradedEmbedQuota, why
	}
	return false, kbcontext.DegradedNone, ""
}
