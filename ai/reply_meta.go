package ai

import (
	"context"
	"strings"

	"encore.app/wabantu/shared/retrieval"
	"encore.app/wabantu/shared/strutil"
	"encore.dev"
	"encore.dev/rlog"
)

func previewText(s string, max int) string {
	s = strings.TrimSpace(s)
	if shouldRedactCustomerPreview() {
		s = retrieval.RedactPII(s)
	}
	if max <= 0 || len(s) <= max {
		return s
	}
	return strutil.TruncateUTF8Ellipsis(s, max)
}

func shouldRedactCustomerPreview() bool {
	meta := encore.Meta()
	switch meta.Environment.Type {
	case "production", "prod", "staging":
		return true
	default:
		return meta.Environment.Cloud != encore.CloudLocal
	}
}

// Path constants live in buyerflow_bridge.go (sourced from internal/buyerflow).

// AiReplyMeta is stored on message.metadata and echoed in structured logs.
type AiReplyMeta struct {
	Reason         string `json:"reason"`
	Path           string `json:"path"`
	Model          string `json:"model,omitempty"`
	Tier           string `json:"tier,omitempty"`
	LLMUsed        bool   `json:"llmUsed"`
	OrderID        string `json:"orderId,omitempty"`
	OrderAction    string `json:"orderAction,omitempty"`
	InboundReplyTo string `json:"inboundReplyTo,omitempty"`
	// GroundReason is why groundLLMReply replaced the model's reply (empty when it was sent as is).
	GroundReason string `json:"groundReason,omitempty"`

	// LLM-first pipeline decision summary (ai/llm_first.go); all empty for the legacy flow.
	Pipeline string `json:"pipeline,omitempty"` // "llm_first"
	Intent   string `json:"intent,omitempty"`   // the model's classification of the turn
	Lang     string `json:"lang,omitempty"`     // locked reply language ("id" or "en")
	// ReplacedReason is why a template replaced the model's reply (cancel confirmation, failed order save).
	ReplacedReason string `json:"replacedReason,omitempty"`
	// RawOutput is the model's last raw output, redacted (phones, emails) and capped at 1000 runes.
	RawOutput string `json:"rawOutput,omitempty"`
	Attempts  int    `json:"attempts,omitempty"` // LLM calls the turn needed (1, or 2 after a corrective retry)
}

func metaFromRoute(reason, path string, route RoutingDecision) AiReplyMeta {
	return AiReplyMeta{
		Reason:  reason,
		Path:    path,
		Model:   route.Model,
		Tier:    route.Tier,
		LLMUsed: path == PathLLM || path == PathLLMTools || path == PathLLMGrounded || path == PathLLMFirst,
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
	if m.GroundReason != "" {
		args = append(args, "groundReason", m.GroundReason)
	}
	if m.Pipeline != "" {
		args = append(args, "pipeline", m.Pipeline, "intent", m.Intent, "lang", m.Lang, "attempts", m.Attempts)
	}
	if m.ReplacedReason != "" {
		args = append(args, "replacedReason", m.ReplacedReason)
	}
	rlog.Info("AI job: outcome", args...)
}

// LogAndRecord logs outcome and persists per-tenant AI activity (usage_event).
func (m AiReplyMeta) LogAndRecord(ctx context.Context, convoID, inboundID string, inputTok, outputTok int) {
	m.LogOutcome(convoID, inboundID)
	recordActivity(ctx, m, "", inputTok, outputTok)
}
