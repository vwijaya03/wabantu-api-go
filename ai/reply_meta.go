package ai

import (
	"context"

	"encore.dev/rlog"
	"strings"

	"encore.app/wabantu/shared/strutil"
)

func previewText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return strutil.TruncateUTF8Ellipsis(s, max)
}

// Delivery path — how the outbound reply was produced (for logs + message.metadata).
const (
	PathProfileIncomplete = "profile_incomplete"
	PathGreeting          = "greeting"
	PathInjectionGuard    = "injection_guard"
	PathEscalate          = "sensitive_escalate"
	PathOutOfScope        = "out_of_scope"
	PathNonQuestion       = "in_scope_non_question"
	PathLowConfidence     = "low_confidence"
	PathOrderFlow         = "order_flow"
	PathOrderCancel       = "order_cancel"
	PathOrderStatus       = "order_status"
	PathOrderLookupDenied = "order_lookup_denied"
	PathRecipientPolicy   = "recipient_policy"
	PathCatalogDB         = "catalog_db"
	PathConsulting        = "consulting"
	PathFAQCache          = "faq_cache"
	PathFAQDirect         = "faq_direct"
	PathLLM               = "llm"
	PathLLMTools          = "llm_tools"
	PathLLMGrounded       = "llm_grounded"
	PathCostLimit         = "cost_limit"
	PathAutoFallback      = "auto_fallback"
	PathPaymentProof        = "payment_proof"
	PathProductImageMatch   = "product_image_match"
	PathImageFallback       = "image_fallback"
)

// AiReplyMeta is stored on message.metadata and echoed in structured logs.
type AiReplyMeta struct {
	Reason      string `json:"reason"`
	Path        string `json:"path"`
	Model       string `json:"model,omitempty"`
	Tier        string `json:"tier,omitempty"`
	LLMUsed     bool   `json:"llmUsed"`
	OrderID     string `json:"orderId,omitempty"`
	OrderAction string `json:"orderAction,omitempty"`
}

func metaFromRoute(reason, path string, route RoutingDecision) AiReplyMeta {
	return AiReplyMeta{
		Reason:  reason,
		Path:    path,
		Model:   route.Model,
		Tier:    route.Tier,
		LLMUsed: path == PathLLM || path == PathLLMTools || path == PathLLMGrounded,
	}
}

func metaNoLLM(reason, path string) AiReplyMeta {
	return AiReplyMeta{Reason: reason, Path: path, LLMUsed: false}
}

// LogOutcome writes one searchable line per auto-reply decision.
func (m AiReplyMeta) LogOutcome(convoID, inboundID string) {
	args := []any{
		"convoId", convoID,
		"inboundId", inboundID,
		"path", m.Path,
		"reason", m.Reason,
		"llmUsed", m.LLMUsed,
	}
	if m.Model != "" {
		args = append(args, "model", m.Model, "tier", m.Tier)
	}
	if m.OrderID != "" {
		args = append(args, "orderId", m.OrderID, "orderAction", m.OrderAction)
	}
	rlog.Info("AI job: outcome", args...)
}

// LogAndRecord logs outcome and persists per-tenant AI activity (usage_event).
func (m AiReplyMeta) LogAndRecord(ctx context.Context, convoID, inboundID string, inputTok, outputTok int) {
	m.LogOutcome(convoID, inboundID)
	recordActivity(ctx, m, "", inputTok, outputTok)
}
