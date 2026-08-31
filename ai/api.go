package ai

import (
	"context"
	"crypto/subtle"

	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"github.com/redis/go-redis/v9"

	"encore.app/wabantu/shared/redisurl"
)

// ─── Encore secrets ──────────────────────────────────────────────────────────

var secrets struct {
	AnthropicAPIKey   string
	RedisURL          string
	AiInternalToken   string
	DataEncryptionKey string
}

// Fallback client defaults (per-request routing uses Haiku/Sonnet in routing.go).
const (
	defaultAnthropicModel   = ModelSonnet
	defaultAnthropicMaxToks = 1024
)

// ─── Singleton service ───────────────────────────────────────────────────────

var svc *AutoReplyService

func init() {
	rdb := newRedisClient()
	client := NewAnthropicClient(secrets.AnthropicAPIKey, AnthropicConfig{
		Model:     defaultAnthropicModel,
		MaxTokens: defaultAnthropicMaxToks,
	})
	svc = NewAutoReplyService(rdb, client)
	rlog.Info("ai model catalog",
		"haiku", DefaultHaikuAPIID(),
		"sonnet", DefaultSonnetAPIID(),
		"activeReply", ActiveModelsForUse(ModelUseAutoReply),
		"summarize", DefaultHaikuAPIID(),
	)
}

func newRedisClient() *redis.Client {
	raw := secrets.RedisURL
	if raw == "" {
		raw = "redis://localhost:6379"
	}
	client, err := redisurl.NewClient(raw)
	if err != nil {
		panic("ai: invalid RedisURL: " + err.Error())
	}
	return client
}

// ─── Request / Response types ────────────────────────────────────────────────

type InternalAutoReplyRequest struct {
	TenantID         string `json:"tenantId"`
	TenantSchema     string `json:"tenantSchema"`
	ConversationID   string `json:"conversationId"`
	InboundMessageID string `json:"inboundMessageId"`
	InternalToken    string `header:"X-Ai-Internal-Token"`
}

type InternalAutoReplyResponse struct {
	Sent bool `json:"sent"`
}

type InternalFallbackRequest struct {
	TenantID         string `json:"tenantId"`
	TenantSchema     string `json:"tenantSchema"`
	ConversationID   string `json:"conversationId"`
	InboundMessageID string `json:"inboundMessageId"`
	InternalToken    string `header:"X-Ai-Internal-Token"`
}

type InternalFallbackResponse struct {
	OK bool `json:"ok"`
}

// ─── Endpoints ───────────────────────────────────────────────────────────────

//encore:api public method=POST path=/api/v1/internal/ai/auto-reply
func InternalProcessAutoReply(ctx context.Context, req *InternalAutoReplyRequest) (*InternalAutoReplyResponse, error) {
	if err := assertInternalToken(req.InternalToken); err != nil {
		return nil, err
	}

	rlog.Info("internal auto-reply called",
		"tenantId", req.TenantID,
		"convoId", req.ConversationID,
		"inboundId", req.InboundMessageID,
	)

	payload := AiReplyJobPayload{
		TenantID:         req.TenantID,
		TenantSchema:     req.TenantSchema,
		ConversationID:   req.ConversationID,
		InboundMessageID: req.InboundMessageID,
	}
	sent, err := svc.ProcessAutoReply(ctx, payload)
	if err != nil {
		rlog.Warn("internal auto-reply failed",
			"err", err,
			"tenantId", req.TenantID,
			"convoId", req.ConversationID,
		)
		return nil, err
	}
	return &InternalAutoReplyResponse{Sent: sent}, nil
}

//encore:api public method=POST path=/api/v1/internal/ai/auto-reply/fallback
func InternalProcessFallback(ctx context.Context, req *InternalFallbackRequest) (*InternalFallbackResponse, error) {
	if err := assertInternalToken(req.InternalToken); err != nil {
		return nil, err
	}

	rlog.Info("internal auto-reply fallback called",
		"tenantId", req.TenantID,
		"convoId", req.ConversationID,
		"inboundId", req.InboundMessageID,
	)

	payload := AiReplyJobPayload{
		TenantID:         req.TenantID,
		TenantSchema:     req.TenantSchema,
		ConversationID:   req.ConversationID,
		InboundMessageID: req.InboundMessageID,
	}
	if err := svc.FallbackAutoReply(ctx, payload); err != nil {
		rlog.Warn("internal auto-reply fallback failed",
			"err", err,
			"tenantId", req.TenantID,
			"convoId", req.ConversationID,
		)
		return nil, err
	}
	return &InternalFallbackResponse{OK: true}, nil
}

// ─── Token validation ────────────────────────────────────────────────────────

func assertInternalToken(token string) error {
	expected := secrets.AiInternalToken
	if expected == "" || token == "" {
		return &errs.Error{Code: errs.Unauthenticated, Message: "Unauthorized internal request"}
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		return &errs.Error{Code: errs.Unauthenticated, Message: "Unauthorized internal request"}
	}
	return nil
}
