package chatwidget

import (
	"context"
	"encoding/json"
	"time"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/internal/chatengine"
	"encore.app/wabantu/shared/publictenant"
)

func persistAssistantReply(
	ctx context.Context,
	ts appdb.TenantScope,
	ref publictenant.TenantRef,
	sessionID, clientID, userBody string,
	out chatengine.Output,
) (*PostMessageResponse, error) {
	var visitorMsgID string
	err := ts.QueryRowContext(ctx, `
		INSERT INTO `+ts.T("web_chat_message")+`
		(session_id, client_message_id, role, body, content_type)
		VALUES ($1::uuid, NULLIF($2, ''), 'visitor', $3, 'text')
		RETURNING id::text`, sessionID, clientID, userBody).Scan(&visitorMsgID)
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
		RETURNING id::text, created_at`, sessionID, out.Body, meta).
		Scan(&replyID, &createdAt)
	if err != nil {
		return nil, err
	}
	_, _ = ts.ExecContext(ctx, `
		UPDATE `+ts.T("web_chat_session")+`
		SET last_message_at = now(), updated_at = now() WHERE id = $1::uuid`, sessionID)

	captureWebChatEvidence(ctx, ref.TenantID, ref.TenantSchema, sessionID, visitorMsgID, replyID, clientID, userBody, out.Body, out.Path, out.Retrieval, out.DegradedMode)

	return &PostMessageResponse{
		Visitor: MessagePair{ID: visitorMsgID},
		Reply: ChatMessage{
			ID: replyID, Role: "assistant", Body: out.Body,
			ContentType: "text", Metadata: meta, CreatedAt: createdAt,
		},
	}, nil
}
