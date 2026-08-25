package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"encore.app/wabantu/auth"
	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/inboxrealtime"
	"encore.app/wabantu/shared/strutil"
)

type UnreadSummaryResponse struct {
	TotalUnreadMessages int `json:"totalUnreadMessages"`
}

type HandoffParams struct {
	Reason *string `json:"reason"`
}

type BulkIDsParams struct {
	IDs    []string `json:"ids"`
	Reason *string  `json:"reason,omitempty"`
}

// GetUnreadSummary returns the total unread message count across conversations.
//
//encore:api auth method=GET path=/api/v1/inbox/unread-summary
func GetUnreadSummary(ctx context.Context) (*UnreadSummaryResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return nil, err
	}

	var sum int
	if err := ts.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(unread_count), 0) FROM conversation`,
	).Scan(&sum); err != nil {
		return nil, apperr.Internal("failed to load unread summary")
	}
	return &UnreadSummaryResponse{TotalUnreadMessages: sum}, nil
}

// MarkConversationRead clears unread_count for a conversation.
//
//encore:api auth method=PATCH path=/api/v1/inbox/conversations/:id/read
func MarkConversationRead(ctx context.Context, id string) error {
	user, err := currentUser()
	if err != nil {
		return err
	}
	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return apperr.Internal("database connection failed")
	}

	res, err := ts.ExecContext(ctx,
		`UPDATE conversation SET unread_count = 0 WHERE id = $1`, id)
	if err != nil {
		return apperr.Internal("failed to mark conversation read")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return apperr.NotFound("Percakapan tidak ditemukan")
	}
	return nil
}

// HandoffConversation assigns the conversation to a human agent.
//
//encore:api auth method=POST path=/api/v1/inbox/conversations/:id/handoff
func HandoffConversation(ctx context.Context, id string, p *HandoffParams) error {
	user, err := currentUser()
	if err != nil {
		return err
	}
	reason := "Diambil alih manual oleh staff"
	if p != nil && p.Reason != nil && strings.TrimSpace(*p.Reason) != "" {
		reason = strings.TrimSpace(*p.Reason)
	}
	reason = strutil.TruncateUTF8(reason, 280)

	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return apperr.Internal("database connection failed")
	}

	var exists bool
	if err := ts.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM conversation WHERE id = $1)`, id,
	).Scan(&exists); err != nil || !exists {
		return apperr.NotFound("Percakapan tidak ditemukan")
	}

	_, err = ts.ExecContext(ctx, `
		UPDATE conversation
		SET ai_handled = false,
		    ai_paused_at = NOW(),
		    assigned_to_user_id = NULL,
		    assigned_to_name = $1,
		    handoff_reason = $2
		WHERE id = $3`,
		user.Email, reason, id)
	if err != nil {
		return apperr.Internal("failed to handoff conversation")
	}

	_, _ = ts.ExecContext(ctx, `
		INSERT INTO message (conversation_id, direction, author, type, body, metadata, status)
		VALUES ($1, 'out', 'system', 'text', 'Staff mengambil alih percakapan ini.', $2::jsonb, 'sent')`,
		id, fmt.Sprintf(`{"reason":%q}`, reason))

	return nil
}

// ResumeAI re-enables AI auto-reply for a conversation.
//
//encore:api auth method=POST path=/api/v1/inbox/conversations/:id/ai-resume
func ResumeAI(ctx context.Context, id string) error {
	user, err := currentUser()
	if err != nil {
		return err
	}
	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return apperr.Internal("database connection failed")
	}

	var exists bool
	if err := ts.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM conversation WHERE id = $1)`, id,
	).Scan(&exists); err != nil || !exists {
		return apperr.NotFound("Percakapan tidak ditemukan")
	}

	_, err = ts.ExecContext(ctx, `
		UPDATE conversation
		SET ai_handled = true,
		    ai_paused_at = NULL,
		    assigned_to_user_id = NULL,
		    assigned_to_name = NULL,
		    handoff_reason = NULL
		WHERE id = $1`, id)
	if err != nil {
		return apperr.Internal("failed to resume AI")
	}

	_, _ = ts.ExecContext(ctx, `
		INSERT INTO message (conversation_id, direction, author, type, body, metadata, status)
		VALUES ($1, 'out', 'system', 'text', 'AI auto-reply diaktifkan kembali.', '{}'::jsonb, 'sent')`,
		id)

	return nil
}

// BulkHandoffConversation assigns multiple conversations to a human agent.
//
//encore:api auth method=POST path=/api/v1/inbox/conversations-batch/handoff
func BulkHandoffConversation(ctx context.Context, req *BulkIDsParams) error {
	user, err := currentUser()
	if err != nil {
		return err
	}
	if req == nil || len(req.IDs) == 0 {
		return apperr.BadRequest("ids required")
	}

	reason := "Diambil alih manual oleh staff"
	if req.Reason != nil && strings.TrimSpace(*req.Reason) != "" {
		reason = strings.TrimSpace(*req.Reason)
	}
	reason = strutil.TruncateUTF8(reason, 280)

	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return apperr.Internal("database connection failed")
	}

	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return apperr.Internal("failed to begin transaction")
	}
	defer tx.Rollback()
	tTx := ts.WithQ(tx)

	for _, id := range req.IDs {
		var exists bool
		if err := tTx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM conversation WHERE id = $1::uuid)`, id,
		).Scan(&exists); err != nil || !exists {
			return apperr.NotFound("Percakapan tidak ditemukan")
		}

		if _, err := tTx.ExecContext(ctx, `
			UPDATE conversation
			SET ai_handled = false,
			    ai_paused_at = NOW(),
			    assigned_to_user_id = NULL,
			    assigned_to_name = $1,
			    handoff_reason = $2
			WHERE id = $3::uuid`, user.Email, reason, id,
		); err != nil {
			return apperr.Internal("failed to handoff conversation")
		}

		if _, err := tTx.ExecContext(ctx, `
			INSERT INTO message (conversation_id, direction, author, type, body, metadata, status)
			VALUES ($1::uuid, 'out', 'system', 'text', 'Staff mengambil alih percakapan ini.', $2::jsonb, 'sent')`,
			id, fmt.Sprintf(`{"reason":%q}`, reason),
		); err != nil {
			return apperr.Internal("failed to create handoff system message")
		}
	}

	if err := tx.Commit(); err != nil {
		return apperr.Internal("failed to commit handoff batch")
	}
	return nil
}

// BulkResumeAI re-enables AI auto-reply for multiple conversations.
//
//encore:api auth method=POST path=/api/v1/inbox/conversations-batch/ai-resume
func BulkResumeAI(ctx context.Context, req *BulkIDsParams) error {
	user, err := currentUser()
	if err != nil {
		return err
	}
	if req == nil || len(req.IDs) == 0 {
		return apperr.BadRequest("ids required")
	}

	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return apperr.Internal("database connection failed")
	}

	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return apperr.Internal("failed to begin transaction")
	}
	defer tx.Rollback()
	tTx := ts.WithQ(tx)

	for _, id := range req.IDs {
		var exists bool
		if err := tTx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM conversation WHERE id = $1::uuid)`, id,
		).Scan(&exists); err != nil || !exists {
			return apperr.NotFound("Percakapan tidak ditemukan")
		}

		if _, err := tTx.ExecContext(ctx, `
			UPDATE conversation
			SET ai_handled = true,
			    ai_paused_at = NULL,
			    assigned_to_user_id = NULL,
			    assigned_to_name = NULL,
			    handoff_reason = NULL
			WHERE id = $1::uuid`, id,
		); err != nil {
			return apperr.Internal("failed to resume AI")
		}

		if _, err := tTx.ExecContext(ctx, `
			INSERT INTO message (conversation_id, direction, author, type, body, metadata, status)
			VALUES ($1::uuid, 'out', 'system', 'text', 'AI auto-reply diaktifkan kembali.', '{}'::jsonb, 'sent')`,
			id,
		); err != nil {
			return apperr.Internal("failed to create resume system message")
		}
	}

	if err := tx.Commit(); err != nil {
		return apperr.Internal("failed to commit resume batch")
	}
	return nil
}

func setInboxStreamCORS(w http.ResponseWriter, r *http.Request) {
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
}

// InboxStream streams inbox activity events via Server-Sent Events (Redis pub/sub).
//
//encore:api public raw method=GET path=/api/v1/inbox/stream
func InboxStream(w http.ResponseWriter, r *http.Request) {
	setInboxStreamCORS(w, r)

	user, err := auth.AuthenticateHTTP(r.Context(), r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !user.CanAccessInbox() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	rdb := auth.RedisClient()
	ctx := r.Context()
	channel := inboxrealtime.Channel(user.TenantID)
	sub := rdb.Subscribe(ctx, channel)
	defer sub.Close()

	ping, _ := json.Marshal(inboxrealtime.PingPayload{Type: "ping"})
	_ = inboxrealtime.WriteSSE(w, ping)
	flusher.Flush()

	pingTicker := time.NewTicker(25 * time.Second)
	defer pingTicker.Stop()

	msgCh := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case <-pingTicker.C:
			_ = inboxrealtime.WriteSSE(w, ping)
			flusher.Flush()
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			if msg.Channel != channel {
				continue
			}
			_ = inboxrealtime.WriteSSE(w, []byte(msg.Payload))
			flusher.Flush()
		}
	}
}
