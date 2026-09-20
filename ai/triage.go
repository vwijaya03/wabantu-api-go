package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	triageMaxMessages  = 200
	triageAnchorBefore = 80
	triageAnchorAfter  = 20
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
	InboundID    string   `json:"inboundId"`
	UserText     string   `json:"userText"`
	ActualPath   string   `json:"actualPath"`
	ExpectedPath string   `json:"expectedPath"`
	Skipped      bool     `json:"skipped,omitempty"`
	SkipReason   string   `json:"skipReason,omitempty"`
	PriorTurns   []string `json:"priorTurns,omitempty"`
	TurnIndex    int      `json:"turnIndex,omitempty"`
}

// TriageRegressionFailure is one failed auto-gen regression case from GHA.
type TriageRegressionFailure struct {
	CaseName     string `json:"caseName"`
	GotPath      string `json:"gotPath"`
	WantPath     string `json:"wantPath"`
	ReplyPreview string `json:"replyPreview,omitempty"`
}

// TriageFixHints guides AI routing fix (Composer / human review).
type TriageFixHints struct {
	LikelyFiles     []string `json:"likelyFiles"`
	CatalogSource   string   `json:"catalogSource"`
	TestUsesFixture string   `json:"testUsesFixture"`
}

// AnalyzeConversationResult summarizes a read-only routing replay.
type AnalyzeConversationResult struct {
	TenantSchema          string                    `json:"tenantSchema"`
	ConversationID        string                    `json:"conversationId"`
	FocusInboundID        string                    `json:"focusInboundId,omitempty"`
	MessagesLoaded        int                       `json:"messagesLoaded"`
	TurnsChecked          int                       `json:"turnsChecked"`
	TurnsSkipped          int                       `json:"turnsSkipped"`
	Mismatches            []TriageMismatch          `json:"mismatches"`
	HasDeterministic      bool                      `json:"hasDeterministicMismatch"`
	FocusFound            bool                      `json:"focusFound,omitempty"`
	RegressionFailures    []TriageRegressionFailure `json:"regressionFailures,omitempty"`
	FixHints              *TriageFixHints           `json:"fixHints,omitempty"`
	SimulatorSnapshot     *TriageSimulatorSnapshot  `json:"simulatorSnapshot,omitempty"`
	CursorAgentID         string                    `json:"cursorAgentId,omitempty"`
	CursorFixGitHubRunURL string                    `json:"cursorFixGithubRunUrl,omitempty"`
	CursorFixAttempts     int                       `json:"cursorFixAttempts,omitempty"`
	VerifyFailures        []TriageRegressionFailure `json:"verifyFailures,omitempty"`
	VerifyNote            string                    `json:"verifyNote,omitempty"`
	VerifyUsedLiveCatalog bool                      `json:"verifyUsedLiveCatalog,omitempty"`
	VerifyPassed          *bool                     `json:"verifyPassed,omitempty"`
}

// EnrichAnalysisResult adds fix hints for UI and AI fix workflows.
func EnrichAnalysisResult(r *AnalyzeConversationResult) {
	if r == nil {
		return
	}
	r.FixHints = &TriageFixHints{
		LikelyFiles:     []string{"internal/buyerflow/*.go", "ai/autoreply.go", "ai/order_flow_handler.go"},
		CatalogSource:   "tenant_db",
		TestUsesFixture: "tenant_catalog_snapshot",
	}
	if r.SimulatorSnapshot == nil {
		r.FixHints.TestUsesFixture = "newOmahSimulator"
	}
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
func BuildSimulatorFromTenant(ctx context.Context, ts tenantScopedQuerier) (*ConversationSimulator, error) {
	profile, err := loadBusinessProfile(ctx, ts)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, fmt.Errorf("business profile not found")
	}
	catalog, err := loadActiveCatalog(ctx, ts, defaultCatalogLoadLimit)
	if err != nil {
		return nil, err
	}
	kb, err := loadKBEntries(ctx, ts, 50)
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
func FetchTriageMessages(ctx context.Context, ts tenantScopedQuerier, conversationID, anchorInboundID string, maxMessages int) ([]TriageMessage, error) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return nil, fmt.Errorf("conversationId required")
	}
	if maxMessages < 1 || maxMessages > triageMaxMessages {
		maxMessages = triageMaxMessages
	}

	if strings.TrimSpace(anchorInboundID) != "" {
		return fetchTriageMessagesAnchored(ctx, ts, conversationID, anchorInboundID, maxMessages)
	}
	return fetchTriageMessagesTail(ctx, ts, conversationID, maxMessages)
}

func fetchTriageMessagesTail(ctx context.Context, ts tenantScopedQuerier, conversationID string, maxMessages int) ([]TriageMessage, error) {
	msgTbl := ts.T("message")
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, direction, COALESCE(body,''), type, metadata, created_at
		FROM (
			SELECT id, direction, body, type, metadata, created_at
			FROM %s
			WHERE conversation_id = $1::uuid
			ORDER BY created_at DESC
			LIMIT $2
		) recent
		ORDER BY created_at ASC`, msgTbl), conversationID, maxMessages)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTriageMessages(rows)
}

func fetchTriageMessagesAnchored(ctx context.Context, ts tenantScopedQuerier, conversationID, anchorInboundID string, maxMessages int) ([]TriageMessage, error) {
	msgTbl := ts.T("message")
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		WITH ordered AS (
			SELECT id, direction, COALESCE(body,'') AS body, type, metadata, created_at,
			       ROW_NUMBER() OVER (ORDER BY created_at ASC) AS rn
			FROM %s
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
		LIMIT $5`, msgTbl),
		conversationID, anchorInboundID, triageAnchorBefore, triageAnchorAfter, maxMessages)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	msgs, err := scanTriageMessages(rows)
	if err != nil {
		return nil, err
	}
	// Missing anchor (hard-deleted inbound) must not fall back to the conversation tail —
	// that would analyze sibling turns and surface the wrong pair in the same thread.
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
	priorTurnInputs := make([]string, 0, 8)

	for i, msg := range messages {
		if !isInboundTriageTurn(msg) {
			continue
		}
		if focusInboundID != "" && msg.ID != focusInboundID {
			continue
		}
		if focusInboundID != "" {
			result.FocusFound = true
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
				PriorTurns:   append([]string{}, priorTurnInputs...),
				TurnIndex:    len(priorTurnInputs),
			})
		}
		priorTurnInputs = append(priorTurnInputs, userText)
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

	ts, err := openTenantScope(ctx, tenantSchema)
	if err != nil {
		return nil, err
	}

	sim, err := BuildSimulatorFromTenant(ctx, ts)
	if err != nil {
		return nil, err
	}
	messages, err := FetchTriageMessages(ctx, ts, conversationID, focusInboundID, triageMaxMessages)
	if err != nil {
		return nil, err
	}

	result := CompareConversationRoutes(sim, messages, focusInboundID)
	result.TenantSchema = tenantSchema
	result.ConversationID = conversationID
	result.FocusInboundID = strings.TrimSpace(focusInboundID)
	result.SimulatorSnapshot = SimulatorToSnapshot(sim, tenantSchema)
	EnrichAnalysisResult(result)
	return result, nil
}

func shouldEmitRegressionCase(m TriageMismatch) bool {
	if m.Skipped || strings.TrimSpace(m.UserText) == "" {
		return false
	}
	_, untrusted := SuggestWantPath(m)
	return !untrusted
}

// CountRegressionMismatches returns how many deterministic routing cases would be emitted.
func CountRegressionMismatches(mismatches []TriageMismatch) int {
	n := 0
	for _, m := range mismatches {
		if shouldEmitRegressionCase(m) {
			n++
		}
	}
	return n
}

func mismatchForInbound(mismatches []TriageMismatch, inboundID string) *TriageMismatch {
	inboundID = strings.TrimSpace(inboundID)
	if inboundID == "" {
		return nil
	}
	for i := range mismatches {
		if mismatches[i].InboundID == inboundID {
			return &mismatches[i]
		}
	}
	return nil
}

// RoutingLoopRejectedReason explains why a focused (or empty) analyze must not
// open a forensic routing job — especially when the reported turn is llm_grounded.
func RoutingLoopRejectedReason(result *AnalyzeConversationResult) string {
	if result == nil || CountRegressionMismatches(result.Mismatches) > 0 {
		return ""
	}
	focus := strings.TrimSpace(result.FocusInboundID)
	if focus == "" {
		return ""
	}
	if !result.FocusFound {
		return "turn yang diminta tidak ada di percakapan (pesan mungkin sudah dihapus). Loop tidak boleh memakai turn lain dalam thread yang sama."
	}
	m := mismatchForInbound(result.Mismatches, focus)
	if m != nil && m.Skipped && m.SkipReason == "non_deterministic_path" {
		return fmt.Sprintf(
			"turn yang dilaporkan %q path=%s — loop routing tidak menilai isi katalog/SKU/teks. Buka tab Insiden; jangan Jalankan loop percakapan.",
			previewText(m.UserText, 80),
			m.ActualPath,
		)
	}
	if m != nil && m.Skipped {
		return fmt.Sprintf(
			"turn %q dilewati (%s). Loop routing tidak menilai turn ini.",
			previewText(m.UserText, 80),
			m.SkipReason,
		)
	}
	path := ""
	text := ""
	if m != nil {
		path = m.ActualPath
		text = m.UserText
	}
	if path == "" {
		path = "sama di WhatsApp dan simulator"
	}
	if text == "" {
		text = focus
	}
	return fmt.Sprintf(
		"turn %q path %s — bukan mismatch routing. Bug isi (katalog/SKU/teks) → tab Insiden; jangan Jalankan loop percakapan.",
		previewText(text, 80),
		path,
	)
}

// GenerateRegressionCases emits Go source for conversation_regression_auto_gen_test.go.
func GenerateRegressionCases(mismatches []TriageMismatch, tenantSchema string, snap *TriageSimulatorSnapshot) string {
	var b strings.Builder
	b.WriteString("package buyerflow\n\n")
	b.WriteString("// Code generated by AI triage loop. Review before merge.\n")
	b.WriteString(fmt.Sprintf("// tenantSchema: %s\n\n", tenantSchema))
	snapConst, err := FormatSnapshotGoConst(snap)
	if err != nil {
		snapConst = `""`
	}
	b.WriteString(fmt.Sprintf("const triageAutoGenSnapshotJSON = %s\n\n", snapConst))
	b.WriteString("func conversationRegressionAutoGenCases() []regressionCase {\n")
	b.WriteString("\treturn []regressionCase{\n")

	added := 0
	for _, m := range mismatches {
		if !shouldEmitRegressionCase(m) {
			continue
		}
		want, _ := SuggestWantPath(m)
		name := regressionCaseName(m.InboundID, added)
		b.WriteString(fmt.Sprintf("\t\t{\n\t\t\tname: %q,\n\t\t\tinput: %q,\n", name, m.UserText))
		if len(m.PriorTurns) > 0 {
			b.WriteString("\t\t\tpriorInputs: []string{")
			for i, p := range m.PriorTurns {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(fmt.Sprintf("%q", p))
			}
			b.WriteString("},\n")
		}
		b.WriteString(fmt.Sprintf("\t\t\twantPath: %s,\n\t\t},\n", pathConstName(want)))
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
	case PathConsulting:
		return "PathConsulting"
	default:
		return fmt.Sprintf("%q", path)
	}
}
