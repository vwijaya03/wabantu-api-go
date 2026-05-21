package ai

import "encore.dev/rlog"

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
	PathFAQCache          = "faq_cache"
	PathFAQDirect         = "faq_direct"
	PathLLM               = "llm"
	PathCostLimit         = "cost_limit"
	PathAutoFallback      = "auto_fallback"
)

// AiReplyMeta is stored on message.metadata and echoed in structured logs.
type AiReplyMeta struct {
	Reason  string `json:"reason"`
	Path    string `json:"path"`
	Model   string `json:"model,omitempty"`
	Tier    string `json:"tier,omitempty"`
	LLMUsed bool   `json:"llmUsed"`
}

func metaFromRoute(reason, path string, route RoutingDecision) AiReplyMeta {
	return AiReplyMeta{
		Reason:  reason,
		Path:    path,
		Model:   route.Model,
		Tier:    route.Tier,
		LLMUsed: path == PathLLM,
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
	rlog.Info("AI job: outcome", args...)
}
