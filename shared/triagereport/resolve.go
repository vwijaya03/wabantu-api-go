package triagereport

import (
	"context"
	"database/sql"
	"strings"

	appdb "encore.app/wabantu/shared/db"
)

// ResolveInboundBeforeOutbound prefers metadata.inboundReplyTo, then latest inbound before outbound.
func ResolveInboundBeforeOutbound(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) appdb.Scannable
}, conversationID, outboundMessageID string) (inboundID, userText string, err error) {
	conversationID = strings.TrimSpace(conversationID)
	outboundMessageID = strings.TrimSpace(outboundMessageID)
	if conversationID == "" || outboundMessageID == "" {
		return "", "", sql.ErrNoRows
	}
	err = q.QueryRowContext(ctx, `
		SELECT inbound.id::text, COALESCE(inbound.body, '')
		FROM message outb
		JOIN message inbound
		  ON inbound.id::text = outb.metadata->>'inboundReplyTo'
		 AND inbound.conversation_id = outb.conversation_id
		WHERE outb.id = $1::uuid
		  AND inbound.direction = 'in'
		LIMIT 1`,
		outboundMessageID,
	).Scan(&inboundID, &userText)
	if err == nil {
		return inboundID, userText, nil
	}
	if err != sql.ErrNoRows {
		return "", "", err
	}
	err = q.QueryRowContext(ctx, `
		SELECT m.id::text, COALESCE(m.body, '')
		FROM message m
		WHERE m.conversation_id = $1::uuid
		  AND m.direction = 'in'
		  AND m.created_at < (SELECT created_at FROM message WHERE id = $2::uuid)
		ORDER BY m.created_at DESC
		LIMIT 1`,
		conversationID, outboundMessageID,
	).Scan(&inboundID, &userText)
	return inboundID, userText, err
}
