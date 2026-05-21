package ai

import (
	"context"

	"encore.app/wabantu/usage"
)

type aiActivityCtxKey struct{}

// ActivityContext carries tenant/conversation ids for usage logging.
type ActivityContext struct {
	TenantSchema     string
	TenantID         string
	ConversationID   string
	InboundMessageID string
	Classifier       string
	RouteReason      string
}

func WithActivityContext(ctx context.Context, ac ActivityContext) context.Context {
	return context.WithValue(ctx, aiActivityCtxKey{}, ac)
}

func ActivityContextFrom(ctx context.Context) (ActivityContext, bool) {
	ac, ok := ctx.Value(aiActivityCtxKey{}).(ActivityContext)
	return ac, ok
}

// recordActivity persists tenant AI activity from reply metadata.
func recordActivity(ctx context.Context, meta AiReplyMeta, purpose string, inputTok, outputTok int) {
	ac, ok := ActivityContextFrom(ctx)
	if !ok || ac.TenantSchema == "" {
		return
	}
	if purpose == "" {
		purpose = usage.PurposeInboundAutoreply
	}
	_ = usage.RecordAIActivity(ctx, usage.AIActivityParams{
		TenantSchema:   ac.TenantSchema,
		TenantID:       ac.TenantID,
		ConversationID: ac.ConversationID,
		InboundID:      ac.InboundMessageID,
		Purpose:        purpose,
		Path:           meta.Path,
		Reason:         meta.Reason,
		Model:          meta.Model,
		Tier:           meta.Tier,
		LLMUsed:        meta.LLMUsed,
		InputTokens:    inputTok,
		OutputTokens:   outputTok,
		RouteReason:    ac.RouteReason,
		Classifier:     ac.Classifier,
	})
}
