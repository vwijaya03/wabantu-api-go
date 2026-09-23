package chatwidget

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
)

type WebChatSessionSummary struct {
	ID            string     `json:"id"`
	Status        string     `json:"status"`
	LastMessageAt *time.Time `json:"lastMessageAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
}

type ListWebChatSessionsResponse struct {
	Items []WebChatSessionSummary `json:"items"`
}

type HandoffRequest struct {
	Reason string `json:"reason,omitempty"`
}

//encore:api auth method=GET path=/api/v1/chat-widget/sessions tag:owner
func ListWebChatSessions(ctx context.Context) (*ListWebChatSessionsResponse, error) {
	u, err := owner(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	rows, err := ts.QueryContext(ctx, `
		SELECT id::text, status, last_message_at, created_at
		FROM `+ts.T("web_chat_session")+`
		WHERE status IN ('active', 'handoff')
		ORDER BY COALESCE(last_message_at, created_at) DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []WebChatSessionSummary
	for rows.Next() {
		var it WebChatSessionSummary
		if err := rows.Scan(&it.ID, &it.Status, &it.LastMessageAt, &it.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return &ListWebChatSessionsResponse{Items: items}, rows.Err()
}

//encore:api auth method=POST path=/api/v1/chat-widget/sessions/:sessionId/handoff tag:owner
func HandoffWebChatSession(ctx context.Context, sessionId string, req *HandoffRequest) error {
	u, err := owner(ctx)
	if err != nil {
		return err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return err
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "staff_handoff"
	}
	meta, _ := json.Marshal(map[string]string{"handoffReason": reason, "handoffBy": u.AccountID})
	var status string
	err = ts.QueryRowContext(ctx, `
		UPDATE `+ts.T("web_chat_session")+`
		SET status = 'handoff', metadata = metadata || $2::jsonb, updated_at = now()
		WHERE id = $1::uuid AND status = 'active'
		RETURNING status`, sessionId, meta).Scan(&status)
	if err == sql.ErrNoRows {
		return appErrs.NotFound("sesi tidak ditemukan atau sudah di-handoff")
	}
	if err != nil {
		return err
	}
	msgMeta, _ := json.Marshal(map[string]string{"path": "handoff", "author": "staff"})
	_, _ = ts.ExecContext(ctx, `
		INSERT INTO `+ts.T("web_chat_message")+`
		(session_id, role, body, content_type, metadata)
		VALUES ($1::uuid, 'assistant', $2, 'text', $3::jsonb)`,
		sessionId,
		"Tim kami akan melanjutkan percakapan ini. Mohon tunggu sebentar.",
		msgMeta)
	return nil
}
