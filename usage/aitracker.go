package usage

import (
	"context"
	"errors"
	"time"

	"encore.dev/rlog"
	"encore.dev/storage/cache"
)

// ---------- cache ----------

var aiCluster = cache.NewCluster("ai-sessions", cache.ClusterConfig{
	EvictionPolicy: cache.AllKeysLRU,
})

type AISession struct {
	Exchanges int       `json:"exchanges"`
	Tokens    int       `json:"tokens"`
	StartedAt time.Time `json:"startedAt"`
}

var sessionKS = cache.NewStructKeyspace[string, AISession](aiCluster, cache.KeyspaceConfig{
	KeyPattern:    "ai:session/:key",
	DefaultExpiry: cache.ExpireIn(24 * time.Hour),
})

const (
	maxExchangesPerSession = 10
	maxTokensPerSession    = 15_000
	tokenAnomalyThreshold = 30_000
)

// TrackAIExchange tracks one exchange in the current AI session.
// Returns current session state and whether a new session was started.
// When newSession==true, the caller should record an ai_conversation usage event.
func TrackAIExchange(ctx context.Context, tenantID, convoID string) (sessionExchanges int, sessionTokens int, newSession bool) {
	key := tenantID + ":" + convoID

	session, err := sessionKS.Get(ctx, key)
	if errors.Is(err, cache.Miss) || err != nil {
		s := AISession{Exchanges: 1, Tokens: 0, StartedAt: time.Now()}
		_ = sessionKS.Set(ctx, key, s)
		return 1, 0, true
	}

	if session.Exchanges >= maxExchangesPerSession || session.Tokens >= maxTokensPerSession {
		s := AISession{Exchanges: 1, Tokens: 0, StartedAt: time.Now()}
		_ = sessionKS.Set(ctx, key, s)
		return 1, 0, true
	}

	session.Exchanges++
	_ = sessionKS.Set(ctx, key, session)
	return session.Exchanges, session.Tokens, false
}

// RecordAITokens adds token count to the current session.
// Flags an anomaly when a single conversation exceeds 30k tokens.
func RecordAITokens(ctx context.Context, tenantID, convoID string, tokens int) {
	key := tenantID + ":" + convoID

	session, err := sessionKS.Get(ctx, key)
	if errors.Is(err, cache.Miss) || err != nil {
		session = AISession{Exchanges: 0, Tokens: tokens, StartedAt: time.Now()}
	} else {
		session.Tokens += tokens
	}

	if session.Tokens > tokenAnomalyThreshold {
		rlog.Warn("token anomaly: conversation exceeds threshold",
			"tenantId", tenantID, "convoId", convoID,
			"totalTokens", session.Tokens, "threshold", tokenAnomalyThreshold)
	}

	_ = sessionKS.Set(ctx, key, session)
}

// CheckAICostLimit returns whether the tenant is allowed to start another AI exchange.
func CheckAICostLimit(ctx context.Context, tenantSchema, tenantID string) (allowed bool, reason string) {
	convAllowed, _, _ := CheckQuota(ctx, tenantSchema, "ai_conversation")
	if !convAllowed {
		return false, "monthly AI conversation quota exceeded"
	}

	tokenAllowed, _, _ := CheckQuota(ctx, tenantSchema, "ai_token")
	if !tokenAllowed {
		return false, "monthly AI token quota exceeded"
	}

	return true, ""
}
