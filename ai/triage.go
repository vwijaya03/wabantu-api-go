package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"encore.app/wabantu/tenant"
)

const (
	triageMaxMessages      = 200
	triageAnchorBefore     = 80
	triageAnchorAfter      = 20
	triageAnomalyDefault   = 50
	triageAnomalyMax       = 100
)

// TriageMessage is one inbox row used for routing replay (read-only).
type TriageMessage struct {
	ID        string          `json:"id"`
	Direction string          `json:"direction"`
	Body      string          `json:"body"`
	Type      string          `json:"type"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

// TriageMismatch is one inbound turn where simulator path differs from production.
type TriageMismatch struct {
	InboundID    string `json:"inboundId"`
	UserText     string `json:"userText"`
	ActualPath   string `json:"actualPath"`
	ExpectedPath string `json:"expectedPath"`
	Skipped      bool   `json:"skipped,omitempty"`
	SkipReason   string `json:"skipReason,omitempty"`
}

// AnalyzeConversationResult summarizes a read-only routing replay.
type AnalyzeConversationResult struct {
	TenantSchema     string           `json:"tenantSchema"`
	ConversationID   string           `json:"conversationId"`
	FocusInboundID   string           `json:"focusInboundId,omitempty"`
	MessagesLoaded   int              `json:"messagesLoaded"`
	TurnsChecked     int              `json:"turnsChecked"`
	TurnsSkipped     int              `json:"turnsSkipped"`
	Mismatches       []TriageMismatch `json:"mismatches"`
	HasDeterministic bool             `json:"hasDeterministicMismatch"`
}

var nonDeterministicTriagePaths = map[string]bool{
	PathLLM:         true,
	PathLLMTools:    true,
	PathLLMGrounded: true,
}

// IsNonDeterministicTriagePath reports paths where simulator cannot assert exact routing.
func IsNonDeterministicTriagePath(path string) bool {
	return nonDeterministicTriagePaths[strings.TrimSpace(path)]
}

// ParseOutboundPath reads message.metadata.path from an outbound AI reply.
func ParseOutboundPath(metadata json.RawMessage) string {
	if len(metadata) == 0 {
		return ""
	}
	var meta AiReplyMeta
	if err := json.Unmarshal(metadata, &meta); err != nil {
		return ""
	}
	return strings.TrimSpace(meta.Path)
}

// BuildSimulatorFromTenant loads profile, catalog, and KB for routing replay.
func BuildSimulatorFromTenant(ctx context.Context, q tenantQuerier) (*ConversationSimulator, error) {
	profile, err := loadBusinessProfile(ctx, q)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, fmt.Errorf("business profile not found")
	}
	catalog, err := loadActiveCatalog(ctx, q, 40)
	if err != nil {
		return nil, err
	}
	kb, err := loadKBEntries(ctx, q, 50)
	if err != nil {
		return nil, err
	}
	return &ConversationSimulator{
		Profile: profile,
		Catalog: catalog,
		KB:      kb,
		ScopeKW: businessScopeKeywords(profile),
	}, nil
}

// FetchTriageMessages loads conversation context with optional anchor around inboundId.
func FetchTriageMessages(ctx context.Context, q tenantQuerier, conversationID, anchorInboundID string, maxMessages int) ([]TriageMessage, error) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return nil, fmt.Errorf("conversationId required")
	}
	if maxMessages < 1 || maxMessages > triageMaxMessages {
		maxMessages = triageMaxMessages
	}

	if strings.TrimSpace(anchorInboundID) != "" {
		return fetchTriageMessagesAnchored(ctx, q, conversationID, anchorInboundID, maxMessages)
	}
	return fetchTriageMessagesTail(ctx, q, conversationID, maxMessages)
}

func fetchTriageMessagesTail(ctx context.Context, q tenantQuerier, conversationID string, maxMessages int) ([]TriageMessage, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, direction, COALESCE(body,''), type, metadata, created_at
		FROM (
			SELECT id, direction, body, type, metadata, created_at
			FROM message
			WHERE conversation_id = $1::uuid
			ORDER BY created_at DESC
			LIMIT $2
		) recent
		ORDER BY created_at ASC`, conversationID, maxMessages)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTriageMessages(rows)
}

func fetchTriageMessagesAnchored(ctx context.Context, q tenantQuerier, conversationID, anchorInboundID string, maxMessages int) ([]TriageMessage, error) {
	rows, err := q.QueryContext(ctx, `
		WITH ordered AS (
			SELECT id, direction, COALESCE(body,'') AS body, type, metadata, created_at,
			       ROW_NUMBER() OVER (ORDER BY created_at ASC) AS rn
			FROM message
			WHERE conversation_id = $1::uuid
		),
		anchor AS (
			SELECT rn FROM ordered WHERE id = $2::uuid
		)
		SELECT id, direction, body, type, metadata, created_at
		FROM ordered
		WHERE rn BETWEEN GREATEST(1, (SELECT rn - $3 FROM anchor))
		             AND (SELECT rn + $4 FROM anchor)
		ORDER BY created_at ASC
		LIMIT $5`,
		conversationID, anchorInboundID, triageAnchorBefore, triageAnchorAfter, maxMessages)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	msgs, err := scanTriageMessages(rows)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return fetchTriageMessagesTail(ctx, q, conversationID, maxMessages)
	}
	return msgs, nil
}

func scanTriageMessages(rows *sql.Rows) ([]TriageMessage, error) {
	out := make([]TriageMessage, 0)
	for rows.Next() {
		var m TriageMessage
		var meta []byte
		if err := rows.Scan(&m.ID, &m.Direction, &m.Body, &m.Type, &meta, &m.CreatedAt); err != nil {
			return nil, err
		}
		if len(meta) > 0 {
			m.Metadata = json.RawMessage(meta)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CompareConversationRoutes replays inbound turns and compares simulator path vs stored metadata.path.
func CompareConversationRoutes(sim *ConversationSimulator, messages []TriageMessage, focusInboundID string) *AnalyzeConversationResult {
	result := &AnalyzeConversationResult{
		MessagesLoaded: len(messages),
		Mismatches:     make([]TriageMismatch, 0),
	}
	if sim == nil {
		return result
	}
	focusInboundID = strings.TrimSpace(focusInboundID)

	for i, msg := range messages {
		if !isInboundTriageTurn(msg) {
			continue
		}
		if focusInboundID != "" && msg.ID != focusInboundID {
			continue
		}

		userText := strings.TrimSpace(msg.Body)
		skipReason := triageSkipReason(msg, userText)
		if skipReason != "" {
			result.TurnsSkipped++
			result.Mismatches = append(result.Mismatches, TriageMismatch{
				InboundID:  msg.ID,
				UserText:   previewText(userText, 120),
				Skipped:    true,
				SkipReason: skipReason,
			})
			continue
		}

		actualPath := findActualPathAfterInbound(messages, i)
		if actualPath == "" {
			result.TurnsSkipped++
			result.Mismatches = append(result.Mismatches, TriageMismatch{
				InboundID:  msg.ID,
				UserText:   previewText(userText, 120),
				Skipped:    true,
				SkipReason: "no_outbound_path",
			})
			continue
		}
		if IsNonDeterministicTriagePath(actualPath) {
			result.TurnsSkipped++
			result.Mismatches = append(result.Mismatches, TriageMismatch{
				InboundID:  msg.ID,
				UserText:   previewText(userText, 120),
				ActualPath: actualPath,
				Skipped:    true,
				SkipReason: "non_deterministic_path",
			})
			continue
		}

		out := sim.Turn(userText)
		result.TurnsChecked++
		if out.Path != actualPath {
			result.HasDeterministic = true
			result.Mismatches = append(result.Mismatches, TriageMismatch{
				InboundID:    msg.ID,
				UserText:     previewText(userText, 120),
				ActualPath:   actualPath,
				ExpectedPath: out.Path,
			})
		}
	}
	return result
}

func isInboundTriageTurn(msg TriageMessage) bool {
	return strings.EqualFold(strings.TrimSpace(msg.Direction), "in")
}

func triageSkipReason(msg TriageMessage, userText string) string {
	msgType := strings.ToLower(strings.TrimSpace(msg.Type))
	if msgType == "image" || msgType == "video" || msgType == "document" {
		if userText == "" {
			return "media_without_caption"
		}
		if IsPaymentProofInbound(msgType, userText) {
			return "payment_proof_pipeline"
		}
	}
	if userText == "" {
		return "empty_inbound_body"
	}
	if !hasLetterOrDigit(userText) {
		return "non_text_inbound"
	}
	return ""
}

func hasLetterOrDigit(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func findActualPathAfterInbound(messages []TriageMessage, inboundIdx int) string {
	for j := inboundIdx + 1; j < len(messages); j++ {
		if !strings.EqualFold(messages[j].Direction, "out") {
			continue
		}
		if path := ParseOutboundPath(messages[j].Metadata); path != "" {
			return path
		}
	}
	return ""
}

// AnalyzeConversation read-only replay for one conversation (cold path; SELECT only).
func AnalyzeConversation(ctx context.Context, tenantSchema, conversationID, focusInboundID string) (*AnalyzeConversationResult, error) {
	tenantSchema = strings.TrimSpace(tenantSchema)
	conversationID = strings.TrimSpace(conversationID)
	if tenantSchema == "" || conversationID == "" {
		return nil, fmt.Errorf("tenantSchema and conversationId required")
	}

	conn, err := openTenantConn(ctx, tenantSchema)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	sim, err := BuildSimulatorFromTenant(ctx, conn)
	if err != nil {
		return nil, err
	}
	messages, err := FetchTriageMessages(ctx, conn, conversationID, focusInboundID, triageMaxMessages)
	if err != nil {
		return nil, err
	}

	result := CompareConversationRoutes(sim, messages, focusInboundID)
	result.TenantSchema = tenantSchema
	result.ConversationID = conversationID
	result.FocusInboundID = strings.TrimSpace(focusInboundID)
	return result, nil
}

// GenerateRegressionCases emits Go source for conversation_regression_auto_gen_test.go.
func GenerateRegressionCases(mismatches []TriageMismatch, tenantSchema string) string {
	var b strings.Builder
	b.WriteString("package ai\n\n")
	b.WriteString("// Code generated by AI triage loop. Review before merge.\n")
	b.WriteString(fmt.Sprintf("// tenantSchema: %s\n\n", tenantSchema))
	b.WriteString("func conversationRegressionAutoGenCases() []conversationRegressionCase {\n")
	b.WriteString("\treturn []conversationRegressionCase{\n")

	added := 0
	for _, m := range mismatches {
		if m.Skipped || m.ExpectedPath == "" || m.UserText == "" {
			continue
		}
		name := regressionCaseName(m.InboundID, added)
		input := escapeGoString(m.UserText)
		b.WriteString(fmt.Sprintf("\t\t{\n\t\t\tname: %q,\n\t\t\tinput: %q,\n\t\t\twantPath: %q,\n\t\t},\n",
			name, input, pathConstName(m.ExpectedPath)))
		added++
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

func regressionCaseName(inboundID string, idx int) string {
	short := inboundID
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("triage_%s_%d", short, idx)
}

func pathConstName(path string) string {
	switch path {
	case PathPaymentFAQ:
		return "PathPaymentFAQ"
	case PathOrderStatus:
		return "PathOrderStatus"
	case PathOrderFlow:
		return "PathOrderFlow"
	case PathCatalogDB:
		return "PathCatalogDB"
	case PathPaymentProof:
		return "PathPaymentProof"
	case PathOrderLookupDenied:
		return "PathOrderLookupDenied"
	case PathGreeting:
		return "PathGreeting"
	case PathFAQDirect:
		return "PathFAQDirect"
	case PathFAQCache:
		return "PathFAQCache"
	default:
		return fmt.Sprintf("%q", path)
	}
}

func escapeGoString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", "").Replace(s)
}

// FetchRecentAIActivityAnomalies lists recent ai_activity rows for superadmin review (read-only).
func FetchRecentAIActivityAnomalies(ctx context.Context, tenantSchema string, limit int) ([]TriageAnomalyEntry, error) {
	tenantSchema = strings.TrimSpace(tenantSchema)
	if tenantSchema == "" {
		return nil, fmt.Errorf("tenantSchema required")
	}
	if limit < 1 || limit > triageAnomalyMax {
		limit = triageAnomalyDefault
	}

	conn, err := tenant.TenantConn(ctx, tenantSchema)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx, `
		SELECT metadata, created_at
		FROM usage_event
		WHERE event_type = $1
		  AND created_at >= now() - interval '1 hour'
		ORDER BY created_at DESC
		LIMIT $2`, "ai_activity", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TriageAnomalyEntry, 0)
	for rows.Next() {
		var metaJSON []byte
		var createdAt time.Time
		if err := rows.Scan(&metaJSON, &createdAt); err != nil {
			return nil, err
		}
		entry := parseAnomalyMetadata(metaJSON, createdAt)
		if entry.ConversationID == "" && entry.InboundID == "" {
			continue
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := enrichAnomalyUserTexts(ctx, conn, out); err != nil {
		return nil, err
	}
	return out, nil
}

func enrichAnomalyUserTexts(ctx context.Context, q tenantQuerier, entries []TriageAnomalyEntry) error {
	if len(entries) == 0 {
		return nil
	}
	ids := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for i := range entries {
		id := strings.TrimSpace(entries[i].InboundID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := q.QueryContext(ctx, `
		SELECT id::text, COALESCE(body, '')
		FROM message
		WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	bodies := make(map[string]string, len(ids))
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			return err
		}
		bodies[id] = body
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range entries {
		if body, ok := bodies[entries[i].InboundID]; ok {
			entries[i].UserText = body
		}
	}
	return nil
}

// TriageAnomalyEntry is one recent AI activity row suggested for review.
type TriageAnomalyEntry struct {
	Path            string    `json:"path"`
	Reason          string    `json:"reason,omitempty"`
	ConversationID  string    `json:"conversationId,omitempty"`
	InboundID       string    `json:"inboundId,omitempty"`
	UserText        string    `json:"userText,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	ReviewSuggested bool      `json:"reviewSuggested"`
}

func parseAnomalyMetadata(metaJSON []byte, createdAt time.Time) TriageAnomalyEntry {
	entry := TriageAnomalyEntry{CreatedAt: createdAt, ReviewSuggested: true}
	if len(metaJSON) == 0 {
		return entry
	}
	var meta map[string]any
	if err := json.Unmarshal(metaJSON, &meta); err != nil {
		return entry
	}
	if v, ok := meta["path"].(string); ok {
		entry.Path = strings.TrimSpace(v)
	}
	if v, ok := meta["reason"].(string); ok {
		entry.Reason = strings.TrimSpace(v)
	}
	if v, ok := meta["conversationId"].(string); ok {
		entry.ConversationID = strings.TrimSpace(v)
	}
	if v, ok := meta["inboundId"].(string); ok {
		entry.InboundID = strings.TrimSpace(v)
	}
	if IsNonDeterministicTriagePath(entry.Path) {
		entry.ReviewSuggested = false
	}
	return entry
}
