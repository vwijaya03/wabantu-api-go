package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"encore.app/wabantu/shared/inboxrealtime"
)

const (
	aiSendDoneKeyPrefix = "ai:send:done:"
	aiSendDoneTTL       = 7 * 24 * time.Hour
	amendIdempotencyTTL = time.Hour
	amendIdempotencyKey = "ai:order-amend:"
)

type outboundDraft struct {
	ID         string
	ExternalID string
}

func metaForSend(meta AiReplyMeta, inboundMessageID string) AiReplyMeta {
	if inboundMessageID != "" {
		meta.InboundReplyTo = inboundMessageID
	}
	return meta
}

func (s *AutoReplyService) aiSendAlreadyDone(ctx context.Context, inboundMessageID string) bool {
	if inboundMessageID == "" || s.rdb == nil {
		return false
	}
	n, err := s.rdb.Exists(ctx, aiSendDoneKeyPrefix+inboundMessageID).Result()
	return err == nil && n > 0
}

func (s *AutoReplyService) markAiSendDone(ctx context.Context, inboundMessageID string) {
	if inboundMessageID == "" || s.rdb == nil {
		return
	}
	_ = s.rdb.Set(ctx, aiSendDoneKeyPrefix+inboundMessageID, "1", aiSendDoneTTL).Err()
}

func (s *AutoReplyService) findOutboundForInbound(
	ctx context.Context,
	ts tenantScopedQuerier,
	inboundMessageID string,
) (outboundDraft, bool, error) {
	var draft outboundDraft
	if inboundMessageID == "" {
		return draft, false, nil
	}
	err := ts.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id::text, COALESCE(external_id, '')
		FROM %s
		WHERE direction = 'out'
		  AND metadata->>'inboundReplyTo' = $1
		ORDER BY created_at DESC
		LIMIT 1`, ts.T("message")),
		inboundMessageID,
	).Scan(&draft.ID, &draft.ExternalID)
	if err == sql.ErrNoRows {
		return draft, false, nil
	}
	if err != nil {
		return draft, false, err
	}
	return draft, true, nil
}

func (s *AutoReplyService) insertPendingOutbound(
	ctx context.Context,
	ts tenantScopedQuerier,
	convoID, author, text string,
	meta AiReplyMeta,
) (string, time.Time, error) {
	metadataJSON, _ := json.Marshal(meta)
	var msgID string
	var createdAt time.Time
	err := ts.QueryRowContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (conversation_id, direction, author, type, body, metadata, status)
		VALUES ($1, 'out', $2, 'text', $3, $4::jsonb, 'pending')
		RETURNING id::text, created_at`, ts.T("message")),
		convoID, author, text, string(metadataJSON),
	).Scan(&msgID, &createdAt)
	return msgID, createdAt, err
}

func (s *AutoReplyService) finalizeOutbound(
	ctx context.Context,
	ts tenantScopedQuerier,
	tenantID, convoID, msgID, externalID, preview string,
	createdAt time.Time,
) error {
	_, err := ts.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s
		SET external_id = $1, status = 'sent'
		WHERE id = $2::uuid`, ts.T("message")),
		externalID, msgID,
	)
	if err != nil {
		return fmt.Errorf("update outbound message: %w", err)
	}
	_, err = ts.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s
		SET last_message_at = $1, last_message_preview = $2, status = 'open'
		WHERE id = $3`, ts.T("conversation")),
		createdAt, preview, convoID,
	)
	if err != nil {
		return fmt.Errorf("update conversation: %w", err)
	}
	if tenantID != "" && s.rdb != nil {
		inboxrealtime.Publish(ctx, s.rdb, tenantID)
	}
	return nil
}

func (s *AutoReplyService) markOutboundFailed(ctx context.Context, ts tenantScopedQuerier, msgID string) {
	if msgID == "" {
		return
	}
	_, _ = ts.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET status = 'failed' WHERE id = $1::uuid`, ts.T("message")), msgID)
}
