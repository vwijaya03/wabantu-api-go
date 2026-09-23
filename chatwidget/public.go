package chatwidget

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"encore.app/wabantu/internal/chatengine"
	appdb "encore.app/wabantu/shared/db"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/publictenant"
)

type PublicConfigResponse struct {
	Enabled        bool            `json:"enabled"`
	WelcomeMessage string          `json:"welcomeMessage,omitempty"`
	PersonaName    string          `json:"personaName,omitempty"`
	Position       string          `json:"position"`
	Locale         string          `json:"locale"`
	Tokens         json.RawMessage `json:"tokens,omitempty"`
}

type CreateSessionResponse struct {
	SessionID      string `json:"sessionId"`
	VisitorToken   string `json:"visitorToken"`
	WelcomeMessage string `json:"welcomeMessage,omitempty"`
}

type PostMessageRequest struct {
	Body            string `json:"body"`
	ClientMessageID string `json:"clientMessageId,omitempty"`
	VisitorToken    string `json:"visitorToken"`
}

type ChatMessage struct {
	ID          string          `json:"id"`
	Role        string          `json:"role"`
	Body        string          `json:"body"`
	ContentType string          `json:"contentType"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
}

type PostMessageResponse struct {
	Visitor MessagePair `json:"visitor"`
	Reply   ChatMessage `json:"reply"`
}

type MessagePair struct {
	ID string `json:"id"`
}

type ListMessagesResponse struct {
	Items []ChatMessage `json:"items"`
}

var secrets struct {
	JWTSecret string
}

var engine = &chatengine.Engine{KB: &chatengine.KBProvider{}}

//encore:api public method=GET path=/api/v1/public/chat/:tenantSlug/config
func GetPublicChatConfig(ctx context.Context, tenantSlug string) (*PublicConfigResponse, error) {
	return publictenant.RunPublicTenant(ctx, tenantSlug, resolveTenantBySlug, func(ref publictenant.TenantRef) (*PublicConfigResponse, error) {
		ts, err := openTenant(ctx, ref.TenantSchema)
		if err != nil {
			return nil, err
		}
		var resp PublicConfigResponse
		var tokens []byte
		err = ts.QueryRowContext(ctx, `
			SELECT is_enabled, COALESCE(welcome_message, ''), COALESCE(persona_name, ''),
			       position, locale, custom_tokens
			FROM `+ts.T("chat_widget_config")+` LIMIT 1`).
			Scan(&resp.Enabled, &resp.WelcomeMessage, &resp.PersonaName, &resp.Position, &resp.Locale, &tokens)
		if err == sql.ErrNoRows {
			return &PublicConfigResponse{Enabled: false, Position: "bottom-right", Locale: "id"}, nil
		}
		if err != nil {
			return nil, err
		}
		if len(tokens) > 0 {
			resp.Tokens = tokens
		}
		return &resp, nil
	})
}

//encore:api public method=POST path=/api/v1/public/chat/:tenantSlug/sessions
func CreatePublicChatSession(ctx context.Context, tenantSlug string) (*CreateSessionResponse, error) {
	return publictenant.RunPublicTenant(ctx, tenantSlug, resolveTenantBySlug, func(ref publictenant.TenantRef) (*CreateSessionResponse, error) {
		ts, err := openTenant(ctx, ref.TenantSchema)
		if err != nil {
			return nil, err
		}
		var enabled bool
		var welcome string
		err = ts.QueryRowContext(ctx, `
			SELECT is_enabled, COALESCE(welcome_message, '')
			FROM `+ts.T("chat_widget_config")+` LIMIT 1`).Scan(&enabled, &welcome)
		if err == sql.ErrNoRows || !enabled {
			return nil, appErrs.Unavailable("chat widget belum diaktifkan")
		}
		if err != nil {
			return nil, err
		}
		var sessionID string
		bootstrapHash := hashVisitorToken(ref.Slug + ":bootstrap:" + time.Now().Format(time.RFC3339Nano))
		err = ts.QueryRowContext(ctx, `
			INSERT INTO `+ts.T("web_chat_session")+` (visitor_token_hash, status)
			VALUES ($1, 'active') RETURNING id::text`, bootstrapHash).Scan(&sessionID)
		if err != nil {
			return nil, err
		}
		raw, hash, err := mintVisitorToken(secrets.JWTSecret, ref.Slug, sessionID)
		if err != nil {
			return nil, err
		}
		_, err = ts.ExecContext(ctx, `
			UPDATE `+ts.T("web_chat_session")+` SET visitor_token_hash = $2, updated_at = now() WHERE id = $1::uuid`,
			sessionID, hash)
		if err != nil {
			return nil, err
		}
		return &CreateSessionResponse{SessionID: sessionID, VisitorToken: raw, WelcomeMessage: welcome}, nil
	})
}

//encore:api public method=POST path=/api/v1/public/chat/:tenantSlug/sessions/:sessionId/messages
func PostPublicChatMessage(ctx context.Context, tenantSlug, sessionId string, req *PostMessageRequest) (*PostMessageResponse, error) {
	return publictenant.RunPublicTenant(ctx, tenantSlug, resolveTenantBySlug, func(ref publictenant.TenantRef) (*PostMessageResponse, error) {
		token := strings.TrimSpace(req.VisitorToken)
		if !verifyVisitorToken(secrets.JWTSecret, ref.Slug, sessionId, token) {
			return nil, appErrs.Unauthenticated("token visitor tidak valid")
		}
		ts, err := openTenant(ctx, ref.TenantSchema)
		if err != nil {
			return nil, err
		}
		body := strings.TrimSpace(req.Body)
		if body == "" {
			return nil, appErrs.BadRequest("pesan kosong")
		}
		var enabled bool
		var welcome, persona string
		err = ts.QueryRowContext(ctx, `
			SELECT is_enabled, COALESCE(welcome_message, ''), COALESCE(persona_name, '')
			FROM `+ts.T("chat_widget_config")+` LIMIT 1`).Scan(&enabled, &welcome, &persona)
		if err != nil || !enabled {
			return nil, appErrs.Unavailable("chat widget belum diaktifkan")
		}

		var visitorMsgID string
		clientID := strings.TrimSpace(req.ClientMessageID)
		if clientID != "" {
			err = ts.QueryRowContext(ctx, `
				SELECT id::text FROM `+ts.T("web_chat_message")+`
				WHERE session_id = $1::uuid AND client_message_id = $2`,
				sessionId, clientID).Scan(&visitorMsgID)
			if err == nil {
				reply, err := latestAssistant(ctx, ts, sessionId)
				if err != nil {
					return nil, err
				}
				return &PostMessageResponse{Visitor: MessagePair{ID: visitorMsgID}, Reply: reply}, nil
			}
			if err != sql.ErrNoRows {
				return nil, err
			}
		}

		var sessionStatus string
		if err := ts.QueryRowContext(ctx, `SELECT status FROM `+ts.T("web_chat_session")+` WHERE id = $1::uuid`, sessionId).Scan(&sessionStatus); err == sql.ErrNoRows {
			return nil, appErrs.NotFound("sesi tidak ditemukan")
		} else if err != nil {
			return nil, err
		}
		if sessionStatus == "handoff" {
			out := chatengine.Output{Path: "handoff", Body: "Tim kami sedang menangani percakapan ini. Mohon tunggu."}
			return persistAssistantReply(ctx, ts, ref, sessionId, clientID, body, out)
		}

		block, degraded, why := abuseGate(ctx, ref.TenantSchema, ref.TenantID, "", body)
		if block {
			return nil, appErrs.BadRequest(why)
		}

		err = ts.QueryRowContext(ctx, `
			INSERT INTO `+ts.T("web_chat_message")+`
			(session_id, client_message_id, role, body, content_type)
			VALUES ($1::uuid, NULLIF($2, ''), 'visitor', $3, 'text')
			RETURNING id::text`, sessionId, clientID, body).Scan(&visitorMsgID)
		if err != nil {
			return nil, err
		}

		out, err := engine.ProcessMessage(ctx, ts, chatengine.Input{
			TenantID: ref.TenantID, TenantSchema: ref.TenantSchema,
			SessionID: sessionId, UserText: body, Enabled: enabled,
			Welcome: welcome, PersonaName: persona, Degraded: degraded,
		})
		if err != nil {
			return nil, err
		}

		meta, _ := json.Marshal(map[string]any{"path": out.Path})
		var replyID string
		var createdAt time.Time
		err = ts.QueryRowContext(ctx, `
			INSERT INTO `+ts.T("web_chat_message")+`
			(session_id, role, body, content_type, metadata)
			VALUES ($1::uuid, 'assistant', $2, 'text', $3::jsonb)
			RETURNING id::text, created_at`, sessionId, out.Body, meta).
			Scan(&replyID, &createdAt)
		if err != nil {
			return nil, err
		}
		_, _ = ts.ExecContext(ctx, `
			UPDATE `+ts.T("web_chat_session")+`
			SET last_message_at = now(), updated_at = now() WHERE id = $1::uuid`, sessionId)

		captureWebChatEvidence(ctx, ref.TenantID, ref.TenantSchema, sessionId, visitorMsgID, replyID, clientID, body, out.Body, out.Path, out.Retrieval, out.DegradedMode)

		return &PostMessageResponse{
			Visitor: MessagePair{ID: visitorMsgID},
			Reply: ChatMessage{
				ID: replyID, Role: "assistant", Body: out.Body,
				ContentType: "text", Metadata: meta, CreatedAt: createdAt,
			},
		}, nil
	})
}

type ListMessagesParams struct {
	VisitorToken string `query:"visitorToken"`
}

//encore:api public method=GET path=/api/v1/public/chat/:tenantSlug/sessions/:sessionId/messages
func ListPublicChatMessages(ctx context.Context, tenantSlug, sessionId string, p *ListMessagesParams) (*ListMessagesResponse, error) {
	return publictenant.RunPublicTenant(ctx, tenantSlug, resolveTenantBySlug, func(ref publictenant.TenantRef) (*ListMessagesResponse, error) {
		token := strings.TrimSpace(p.VisitorToken)
		if !verifyVisitorToken(secrets.JWTSecret, ref.Slug, sessionId, token) {
			return nil, appErrs.Unauthenticated("token visitor tidak valid")
		}
		ts, err := openTenant(ctx, ref.TenantSchema)
		if err != nil {
			return nil, err
		}
		rows, err := ts.QueryContext(ctx, `
			SELECT id::text, role, body, content_type, metadata, created_at
			FROM `+ts.T("web_chat_message")+`
			WHERE session_id = $1::uuid
			ORDER BY created_at ASC LIMIT 200`, sessionId)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var items []ChatMessage
		for rows.Next() {
			var m ChatMessage
			if err := rows.Scan(&m.ID, &m.Role, &m.Body, &m.ContentType, &m.Metadata, &m.CreatedAt); err != nil {
				return nil, err
			}
			items = append(items, m)
		}
		return &ListMessagesResponse{Items: items}, rows.Err()
	})
}

func latestAssistant(ctx context.Context, ts appdb.TenantScope, sessionID string) (ChatMessage, error) {
	var m ChatMessage
	err := ts.QueryRowContext(ctx, `
		SELECT id::text, role, body, content_type, metadata, created_at
		FROM `+ts.T("web_chat_message")+`
		WHERE session_id = $1::uuid AND role = 'assistant'
		ORDER BY created_at DESC LIMIT 1`, sessionID).
		Scan(&m.ID, &m.Role, &m.Body, &m.ContentType, &m.Metadata, &m.CreatedAt)
	return m, err
}
