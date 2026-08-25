package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"encore.app/wabantu/tenant"
)

const (
	triageLLMMaxWindow   = 6 * time.Hour
	triageLLMMaxMessages = 100
	triageLLMMaxTurns    = 30
)

// AITriageTurn is one inbound message paired with the following AI outbound reply.
type AITriageTurn struct {
	ConversationID string    `json:"conversationId"`
	InboundID      string    `json:"inboundId"`
	UserText       string    `json:"userText"`
	ReplyText      string    `json:"replyText"`
	Path           string    `json:"path"`
	InboundAt      time.Time `json:"inboundAt"`
}

// LLMScanParams configures a read-only LLM judge scan (cold path).
type LLMScanParams struct {
	TenantID       string
	TenantSchema   string
	ConversationID string
	From           time.Time
	To             time.Time
	MaxTurns       int
}

// LLMScanRunResult summarizes one completed scan execution.
type LLMScanRunResult struct {
	TurnsChecked  int
	FindingsCount int
	InputTokens   int
	OutputTokens  int
	Findings      []LLMScanFinding
}

// LLMScanFinding is one judged turn persisted for superadmin review.
type LLMScanFinding struct {
	AITriageTurn
	Flagged  bool   `json:"flagged"`
	Severity string `json:"severity,omitempty"`
	Category string `json:"category,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type windowMessage struct {
	TriageMessage
	ConversationID string
}

// TriageLLMMaxWindow returns the maximum allowed scan window.
func TriageLLMMaxWindow() time.Duration {
	return triageLLMMaxWindow
}

// ValidateLLMScanWindow ensures from/to are valid for triage scan.
func ValidateLLMScanWindow(from, to time.Time) error {
	if to.Before(from) {
		return fmt.Errorf("window end must be after start")
	}
	if to.Sub(from) > triageLLMMaxWindow {
		return fmt.Errorf("window exceeds maximum %s", triageLLMMaxWindow)
	}
	return nil
}

// FetchAITurnsInWindow loads AI reply turns in a time range (SELECT only).
func FetchAITurnsInWindow(ctx context.Context, tenantSchema string, from, to time.Time, conversationID string, maxTurns int) ([]AITriageTurn, error) {
	tenantSchema = strings.TrimSpace(tenantSchema)
	if tenantSchema == "" {
		return nil, fmt.Errorf("tenantSchema required")
	}
	if err := ValidateLLMScanWindow(from, to); err != nil {
		return nil, err
	}
	if maxTurns < 1 || maxTurns > triageLLMMaxTurns {
		maxTurns = triageLLMMaxTurns
	}

	conn, err := tenant.TenantConn(ctx, tenantSchema)
	if err != nil {
		return nil, err
	}
	defer tenant.CloseTenantConn(conn)

	messages, err := fetchMessagesInWindow(ctx, conn, from, to, strings.TrimSpace(conversationID), triageLLMMaxMessages)
	if err != nil {
		return nil, err
	}
	return extractAITurnsFromWindowMessages(messages, maxTurns), nil
}

func fetchMessagesInWindow(ctx context.Context, q tenantQuerier, from, to time.Time, conversationID string, limit int) ([]windowMessage, error) {
	var rows *sql.Rows
	var err error
	if conversationID != "" {
		rows, err = q.QueryContext(ctx, `
			SELECT id::text, conversation_id::text, direction, COALESCE(body,''), type, metadata, created_at
			FROM message
			WHERE conversation_id = $1::uuid
			  AND created_at >= $2
			  AND created_at <= $3
			ORDER BY created_at ASC
			LIMIT $4`, conversationID, from, to, limit)
	} else {
		rows, err = q.QueryContext(ctx, `
			SELECT id::text, conversation_id::text, direction, COALESCE(body,''), type, metadata, created_at
			FROM message
			WHERE created_at >= $1
			  AND created_at <= $2
			ORDER BY created_at ASC
			LIMIT $3`, from, to, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWindowMessages(rows)
}

func scanWindowMessages(rows *sql.Rows) ([]windowMessage, error) {
	out := make([]windowMessage, 0)
	for rows.Next() {
		var m windowMessage
		var meta []byte
		if err := rows.Scan(
			&m.ID, &m.ConversationID, &m.Direction, &m.Body, &m.Type, &meta, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		if len(meta) > 0 {
			m.Metadata = json.RawMessage(meta)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func extractAITurnsFromWindowMessages(messages []windowMessage, maxTurns int) []AITriageTurn {
	if len(messages) == 0 {
		return nil
	}
	byConversation := make(map[string][]windowMessage)
	for _, m := range messages {
		if m.ConversationID == "" {
			continue
		}
		byConversation[m.ConversationID] = append(byConversation[m.ConversationID], m)
	}

	convIDs := make([]string, 0, len(byConversation))
	for id := range byConversation {
		convIDs = append(convIDs, id)
	}
	sort.Strings(convIDs)

	out := make([]AITriageTurn, 0, maxTurns)
	for _, convID := range convIDs {
		turns := extractAITurnsForConversationWindow(byConversation[convID], convID, maxTurns-len(out))
		out = append(out, turns...)
		if len(out) >= maxTurns {
			break
		}
	}
	return out
}

func extractAITurnsForConversationWindow(messages []windowMessage, conversationID string, maxTurns int) []AITriageTurn {
	if maxTurns < 1 {
		return nil
	}
	plain := make([]TriageMessage, len(messages))
	for i := range messages {
		plain[i] = messages[i].TriageMessage
	}

	out := make([]AITriageTurn, 0, maxTurns)
	for i, msg := range messages {
		if !isInboundTriageTurn(msg.TriageMessage) {
			continue
		}
		userText := strings.TrimSpace(msg.Body)
		if triageSkipReason(msg.TriageMessage, userText) != "" {
			continue
		}
		path := findActualPathAfterInbound(plain, i)
		if path == "" {
			continue
		}
		replyText := findReplyBodyAfterInbound(plain, i)
		out = append(out, AITriageTurn{
			ConversationID: conversationID,
			InboundID:      msg.ID,
			UserText:       userText,
			ReplyText:      replyText,
			Path:           path,
			InboundAt:      msg.CreatedAt,
		})
		if len(out) >= maxTurns {
			break
		}
	}
	return out
}

func findReplyBodyAfterInbound(messages []TriageMessage, inboundIdx int) string {
	for j := inboundIdx + 1; j < len(messages); j++ {
		if !strings.EqualFold(messages[j].Direction, "out") {
			continue
		}
		return strings.TrimSpace(messages[j].Body)
	}
	return ""
}

// RunLLMTriageScan fetches turns and judges each with Haiku (cold path).
func RunLLMTriageScan(ctx context.Context, p LLMScanParams) (*LLMScanRunResult, error) {
	if err := ValidateLLMScanWindow(p.From, p.To); err != nil {
		return nil, err
	}
	maxTurns := p.MaxTurns
	if maxTurns < 1 || maxTurns > triageLLMMaxTurns {
		maxTurns = triageLLMMaxTurns
	}

	turns, err := FetchAITurnsInWindow(ctx, p.TenantSchema, p.From, p.To, p.ConversationID, maxTurns)
	if err != nil {
		return nil, err
	}

	conn, err := tenant.TenantConn(ctx, p.TenantSchema)
	if err != nil {
		return nil, err
	}
	defer tenant.CloseTenantConn(conn)

	businessName := ""
	if profile, err := loadBusinessProfile(ctx, conn); err == nil && profile != nil {
		businessName = strings.TrimSpace(profile.BusinessName)
	}

	catalog, err := loadActiveCatalog(ctx, conn, 40)
	if err != nil {
		catalog = nil
	}

	result := &LLMScanRunResult{
		Findings: make([]LLMScanFinding, 0, len(turns)),
	}

	pending := make([]AITriageTurn, 0, len(turns))
	for _, turn := range turns {
		result.TurnsChecked++
		if det := tryDeterministicJudge(turn, catalog); det.Resolved {
			appendLLMScanFinding(result, turn, det.Verdict)
			_ = recordTriageJudgeActivity(ctx, p.TenantSchema, p.TenantID, turn, det.Verdict, CompletionUsage{}, false)
			continue
		}
		pending = append(pending, turn)
	}

	for i := 0; i < len(pending); i += triageJudgeBatchSize {
		end := i + triageJudgeBatchSize
		if end > len(pending) {
			end = len(pending)
		}
		batch := pending[i:end]
		verdicts, tok, err := judgeTriageTurnBatch(ctx, businessName, catalog, batch)
		result.InputTokens += tok.InputTokens
		result.OutputTokens += tok.OutputTokens
		if err != nil {
			for _, turn := range batch {
				result.Findings = append(result.Findings, LLMScanFinding{
					AITriageTurn: turn,
					Flagged:      false,
					Reason:       "judge_error: " + err.Error(),
				})
			}
			continue
		}
		for j, turn := range batch {
			appendLLMScanFinding(result, turn, verdicts[j])
			_ = recordTriageJudgeActivity(ctx, p.TenantSchema, p.TenantID, turn, verdicts[j], tok, true)
		}
	}
	return result, nil
}

func appendLLMScanFinding(result *LLMScanRunResult, turn AITriageTurn, verdict llmJudgeVerdict) {
	finding := LLMScanFinding{
		AITriageTurn: turn,
		Flagged:      verdict.Flagged,
		Severity:     verdict.Severity,
		Category:     verdict.Category,
		Reason:       verdict.Reason,
	}
	if finding.Flagged {
		result.FindingsCount++
	}
	result.Findings = append(result.Findings, finding)
}
