package admin

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/system"
)

const (
	llmScanStatusPending = "pending"
	llmScanStatusRunning = "running"
	llmScanStatusDone    = "done"
	llmScanStatusFailed  = "failed"

	llmScanMaxConcurrent = 2
)

// ---------- Types ----------

type CreateAITriageLLMScanParams struct {
	TenantID       string `json:"tenantId"`
	ConversationID string `json:"conversationId,omitempty"`
	From           string `json:"from"`
	To             string `json:"to"`
}

type AITriageLLMScan struct {
	ID              string     `json:"id"`
	TenantID        string     `json:"tenantId"`
	TenantSchema    string     `json:"tenantSchema"`
	ConversationID  string     `json:"conversationId,omitempty"`
	From            time.Time  `json:"from"`
	To              time.Time  `json:"to"`
	Status          string     `json:"status"`
	TurnsChecked    int        `json:"turnsChecked"`
	FindingsCount   int        `json:"findingsCount"`
	InputTokens     int        `json:"inputTokens"`
	OutputTokens    int        `json:"outputTokens"`
	ErrorText       string     `json:"errorText,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
	Findings        []AITriageLLMFinding `json:"findings,omitempty"`
}

type AITriageLLMFinding struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversationId"`
	InboundID      string    `json:"inboundId"`
	UserText       string    `json:"userText,omitempty"`
	ReplyText      string    `json:"replyText,omitempty"`
	Path           string    `json:"path,omitempty"`
	Flagged        bool      `json:"flagged"`
	Severity       string    `json:"severity,omitempty"`
	Category       string    `json:"category,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	InboundAt      time.Time `json:"inboundAt"`
}

type CreateAITriageLLMScanResponse struct {
	Scan AITriageLLMScan `json:"scan"`
}

type GetAITriageLLMScanResponse struct {
	Scan AITriageLLMScan `json:"scan"`
}

// CreateAITriageLLMScan starts an async LLM judge scan for a time window (superadmin only).
//
//encore:api auth method=POST path=/api/v1/admin/ai-triage/llm-scans tag:super_admin
func CreateAITriageLLMScan(ctx context.Context, p *CreateAITriageLLMScanParams) (*CreateAITriageLLMScanResponse, error) {
	user, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if p == nil || strings.TrimSpace(p.TenantID) == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "tenantId required"}
	}
	from, to, err := parseLLMScanWindow(p.From, p.To)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}
	if err := ai.ValidateLLMScanWindow(from, to); err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}

	active, err := countActiveLLMScans(ctx)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "check llm scan queue failed"}
	}
	if active >= llmScanMaxConcurrent {
		return nil, &errs.Error{Code: errs.ResourceExhausted, Message: "max concurrent llm scans reached (2)"}
	}

	schema, err := resolveTenantSchema(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}

	scanID, err := insertLLMScan(ctx, llmScanInsert{
		TenantID:       p.TenantID,
		TenantSchema:   schema,
		ConversationID: strings.TrimSpace(p.ConversationID),
		From:           from,
		To:             to,
		StartedBy:      user.AccountID,
	})
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "create llm scan failed"}
	}

	go runLLMScanAsync(scanID, p.TenantID, schema, strings.TrimSpace(p.ConversationID), from, to)

	scan, err := loadLLMScan(ctx, scanID, false)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "load llm scan failed"}
	}
	return &CreateAITriageLLMScanResponse{Scan: scan}, nil
}

// GetAITriageLLMScan returns scan status and findings for polling.
//
//encore:api auth method=GET path=/api/v1/admin/ai-triage/llm-scans/:id tag:super_admin
func GetAITriageLLMScan(ctx context.Context, id string) (*GetAITriageLLMScanResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	scan, err := loadLLMScan(ctx, strings.TrimSpace(id), true)
	if err != nil {
		return nil, err
	}
	return &GetAITriageLLMScanResponse{Scan: scan}, nil
}

// ---------- DB + async ----------

type llmScanInsert struct {
	TenantID       string
	TenantSchema   string
	ConversationID string
	From           time.Time
	To             time.Time
	StartedBy      string
}

func parseLLMScanWindow(fromStr, toStr string) (time.Time, time.Time, error) {
	fromStr = strings.TrimSpace(fromStr)
	toStr = strings.TrimSpace(toStr)
	if fromStr == "" || toStr == "" {
		return time.Time{}, time.Time{}, errf("from and to required (RFC3339)")
	}
	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		return time.Time{}, time.Time{}, errf("invalid from timestamp")
	}
	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		return time.Time{}, time.Time{}, errf("invalid to timestamp")
	}
	return from.UTC(), to.UTC(), nil
}

func errf(msg string) error {
	return &errs.Error{Code: errs.InvalidArgument, Message: msg}
}

func countActiveLLMScans(ctx context.Context) (int, error) {
	var n int
	err := system.DB.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM ai_triage_llm_scan
		WHERE status IN ($1, $2)`, llmScanStatusPending, llmScanStatusRunning,
	).Scan(&n)
	return n, err
}

func insertLLMScan(ctx context.Context, in llmScanInsert) (string, error) {
	var id string
	var conv any
	if in.ConversationID != "" {
		conv = in.ConversationID
	}
	err := system.DB.QueryRow(ctx, `
		INSERT INTO ai_triage_llm_scan (
			tenant_id, tenant_schema, conversation_id,
			window_from, window_to, status, started_by
		) VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7::uuid)
		RETURNING id::text`,
		in.TenantID, in.TenantSchema, conv, in.From, in.To, llmScanStatusPending, in.StartedBy,
	).Scan(&id)
	return id, err
}

func updateLLMScanRunning(ctx context.Context, scanID string) error {
	_, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_llm_scan
		SET status = $2, updated_at = now()
		WHERE id = $1::uuid`, scanID, llmScanStatusRunning)
	return err
}

func completeLLMScan(ctx context.Context, scanID, status string, result *ai.LLMScanRunResult, errText string) error {
	var completed any
	if status == llmScanStatusDone || status == llmScanStatusFailed {
		completed = time.Now().UTC()
	}
	turns := 0
	findings := 0
	inTok := 0
	outTok := 0
	if result != nil {
		turns = result.TurnsChecked
		findings = result.FindingsCount
		inTok = result.InputTokens
		outTok = result.OutputTokens
	}
	_, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_llm_scan
		SET status = $2,
		    turns_checked = $3,
		    findings_count = $4,
		    input_tokens = $5,
		    output_tokens = $6,
		    error_text = NULLIF($7, ''),
		    updated_at = now(),
		    completed_at = $8
		WHERE id = $1::uuid`,
		scanID, status, turns, findings, inTok, outTok, errText, completed,
	)
	return err
}

func insertLLMFinding(ctx context.Context, scanID string, f ai.LLMScanFinding) error {
	_, err := system.DB.Exec(ctx, `
		INSERT INTO ai_triage_llm_finding (
			scan_id, conversation_id, inbound_id,
			user_text, reply_text, path,
			flagged, severity, category, reason, inbound_at
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid,
			NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''),
			$7, NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), $11
		)`,
		scanID, f.ConversationID, f.InboundID,
		f.UserText, f.ReplyText, f.Path,
		f.Flagged, f.Severity, f.Category, f.Reason, f.InboundAt,
	)
	return err
}

func loadLLMScan(ctx context.Context, scanID string, withFindings bool) (AITriageLLMScan, error) {
	var scan AITriageLLMScan
	var conv sql.NullString
	var errText sql.NullString
	var completed sql.NullTime

	err := system.DB.QueryRow(ctx, `
		SELECT id::text, tenant_id::text, tenant_schema, conversation_id::text,
		       window_from, window_to, status,
		       turns_checked, findings_count, input_tokens, output_tokens,
		       error_text, created_at, updated_at, completed_at
		FROM ai_triage_llm_scan
		WHERE id = $1::uuid`, scanID,
	).Scan(
		&scan.ID, &scan.TenantID, &scan.TenantSchema, &conv,
		&scan.From, &scan.To, &scan.Status,
		&scan.TurnsChecked, &scan.FindingsCount, &scan.InputTokens, &scan.OutputTokens,
		&errText, &scan.CreatedAt, &scan.UpdatedAt, &completed,
	)
	if err == sql.ErrNoRows {
		return scan, &errs.Error{Code: errs.NotFound, Message: "llm scan not found"}
	}
	if err != nil {
		return scan, err
	}
	if conv.Valid {
		scan.ConversationID = conv.String
	}
	if errText.Valid {
		scan.ErrorText = errText.String
	}
	if completed.Valid {
		t := completed.Time
		scan.CompletedAt = &t
	}
	if withFindings {
		findings, err := loadLLMFindings(ctx, scanID)
		if err != nil {
			return scan, err
		}
		scan.Findings = findings
	}
	return scan, nil
}

func loadLLMFindings(ctx context.Context, scanID string) ([]AITriageLLMFinding, error) {
	rows, err := system.DB.Query(ctx, `
		SELECT id::text, conversation_id::text, inbound_id::text,
		       COALESCE(user_text,''), COALESCE(reply_text,''), COALESCE(path,''),
		       flagged, COALESCE(severity,''), COALESCE(category,''), COALESCE(reason,''),
		       inbound_at
		FROM ai_triage_llm_finding
		WHERE scan_id = $1::uuid
		ORDER BY flagged DESC, inbound_at DESC`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AITriageLLMFinding, 0)
	for rows.Next() {
		var f AITriageLLMFinding
		if err := rows.Scan(
			&f.ID, &f.ConversationID, &f.InboundID,
			&f.UserText, &f.ReplyText, &f.Path,
			&f.Flagged, &f.Severity, &f.Category, &f.Reason,
			&f.InboundAt,
		); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func runLLMScanAsync(scanID, tenantID, schema, conversationID string, from, to time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := updateLLMScanRunning(ctx, scanID); err != nil {
		rlog.Error("llm scan status running failed", "scanId", scanID, "err", err)
		_ = completeLLMScan(ctx, scanID, llmScanStatusFailed, nil, err.Error())
		return
	}

	result, err := ai.RunLLMTriageScan(ctx, ai.LLMScanParams{
		TenantID:       tenantID,
		TenantSchema:   schema,
		ConversationID: conversationID,
		From:           from,
		To:             to,
	})
	if err != nil {
		rlog.Warn("llm scan failed", "scanId", scanID, "err", err)
		_ = completeLLMScan(ctx, scanID, llmScanStatusFailed, nil, err.Error())
		return
	}

	for _, f := range result.Findings {
		if err := insertLLMFinding(ctx, scanID, f); err != nil {
			rlog.Warn("llm scan insert finding", "scanId", scanID, "err", err)
		}
	}

	if err := completeLLMScan(ctx, scanID, llmScanStatusDone, result, ""); err != nil {
		rlog.Error("llm scan complete update failed", "scanId", scanID, "err", err)
		return
	}
	rlog.Info("llm scan done", "scanId", scanID, "turns", result.TurnsChecked, "flagged", result.FindingsCount)
}
