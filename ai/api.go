package ai

import (
	"context"
	"crypto/subtle"
	"fmt"

	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"github.com/redis/go-redis/v9"
)

// ─── Encore secrets ──────────────────────────────────────────────────────────

var secrets struct {
	AnthropicApiKey  string
	RedisURL         string
	AiInternalToken  string
	AnthropicModel   string // optional override, default claude-sonnet-4-5
	AnthropicMaxToks string // optional override, default "512"
}

// ─── Singleton service ───────────────────────────────────────────────────────

var svc *AutoReplyService

func init() {
	rdb := newRedisClient()

	model := secrets.AnthropicModel
	if model == "" {
		model = "claude-sonnet-4-5-20250514"
	}
	maxTok := 512
	if secrets.AnthropicMaxToks != "" {
		var n int
		if _, err := fmt.Sscanf(secrets.AnthropicMaxToks, "%d", &n); err == nil && n > 0 {
			maxTok = n
		}
	}

	client := NewAnthropicClient(secrets.AnthropicApiKey, AnthropicConfig{
		Model:     model,
		MaxTokens: maxTok,
	})
	svc = NewAutoReplyService(rdb, client)
}

func newRedisClient() *redis.Client {
	raw := secrets.RedisURL
	if raw == "" {
		raw = "redis://localhost:6379"
	}
	opt, err := redis.ParseURL(raw)
	if err != nil {
		opt = &redis.Options{Addr: raw}
	}
	return redis.NewClient(opt)
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

//encore:api public method=POST path=/internal/ai/auto-reply
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

//encore:api public method=POST path=/internal/ai/auto-reply/fallback
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
