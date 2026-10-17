package usage

import (
	"context"
	"encoding/json"
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
	tokenAnomalyThreshold  = 30_000
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

// ---------- LLM-first token analytics ----------

// EventAITokenLLMFirst is the usage event of the LLM-first pipeline (live and shadow). It is for
// measuring that pipeline per message only: planQuotas has no entry for it, so CheckQuota never
// limits on it and the plan quotas of ai_token stay exactly as they were.
const EventAITokenLLMFirst = "ai_token_llm_first"

// LLMFirstTokens describes one LLM-first turn for the ai_token_llm_first event.
type LLMFirstTokens struct {
	Mode             string `json:"mode,omitempty"`     // "on" (live) or "shadow"
	Pipeline         string `json:"pipeline"`           // "llm_first" (live) or "llm_first_shadow"
	TenantID         string `json:"tenantId,omitempty"` // the event lives in the tenant schema; kept for exports
	ConversationID   string `json:"conversationId,omitempty"`
	InboundMessageID string `json:"inboundMessageId,omitempty"`
	Provider         string `json:"provider,omitempty"`
	Model            string `json:"model,omitempty"`
	Intent           string `json:"intent,omitempty"`
	Fallback         bool   `json:"fallback"`
	Attempts         int    `json:"attempts,omitempty"`
	InputTokens      int    `json:"inputTokens"`
	OutputTokens     int    `json:"outputTokens"`
	ThoughtsTokens   int    `json:"thoughtsTokens"`
	// Filled by RecordLLMFirstTokens from pricingTable (never set by callers): thinking is billed as output.
	EstimatedCostUSD *float64 `json:"estimatedCostUsd,omitempty"` // absent when the price is unknown
	CostKnown        bool     `json:"costKnown"`
	CostNote         string   `json:"costNote,omitempty"` // "harga belum diketahui" when CostKnown is false
}

// CostUnknownNote is what the dashboards and the event metadata say for a model without a price.
const CostUnknownNote = "harga belum diketahui"

// llmFirstEventMeta is the metadata JSON of the event, with the cost estimate. A model without a
// price gets costKnown=false and a note instead of a 0 that would read as "free".
func llmFirstEventMeta(t LLMFirstTokens) ([]byte, error) {
	cost, known := EstimateTokenCostKnown(t.Model, t.InputTokens, t.OutputTokens+t.ThoughtsTokens)
	t.CostKnown = known
	if known {
		t.EstimatedCostUSD, t.CostNote = &cost, ""
	} else {
		t.EstimatedCostUSD, t.CostNote = nil, CostUnknownNote
	}
	return json.Marshal(t)
}

// RecordLLMFirstTokens writes one ai_token_llm_first event (quantity = input + output + thinking)
// with the breakdown in its metadata. Nothing is written for a turn that spent no tokens.
func RecordLLMFirstTokens(ctx context.Context, tenantSchema string, t LLMFirstTokens) error {
	total := t.InputTokens + t.OutputTokens + t.ThoughtsTokens
	if total <= 0 {
		return nil
	}
	meta, err := llmFirstEventMeta(t)
	if err != nil {
		return err
	}
	return RecordEvent(ctx, tenantSchema, EventAITokenLLMFirst, total, meta)
}
