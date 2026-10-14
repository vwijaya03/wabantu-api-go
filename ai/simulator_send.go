package ai

import (
	"context"
	"fmt"
	"time"

	"encore.dev/rlog"

	"encore.app/wabantu/internal/realtime"
	sim "encore.app/wabantu/internal/simulator"
	"encore.app/wabantu/shared/strutil"
	"encore.app/wabantu/shared/whatsappchannel"
)

// sendSimulatorMessage persists an outbound AI/system message for the judge simulator channel.
// Same idempotency/persistence as the Meta path (pending → finalize), but no Meta call.
func (s *AutoReplyService) sendSimulatorMessage(
	ctx context.Context,
	ts tenantScopedQuerier,
	tenantID string,
	convo *dbConversation,
	text, author, inboundMessageID string,
	meta AiReplyMeta,
) error {
	if len(author) > 10 {
		return fmt.Errorf("invalid message author %q (max 10 chars)", author)
	}
	if inboundMessageID != "" && s.aiSendAlreadyDone(ctx, inboundMessageID) {
		return nil
	}
	draft, found, err := s.findOutboundForInbound(ctx, ts, inboundMessageID)
	if err != nil {
		return fmt.Errorf("lookup outbound draft: %w", err)
	}
	if found && draft.ExternalID != "" {
		s.markAiSendDone(ctx, inboundMessageID)
		return nil
	}
	var msgID string
	var createdAt time.Time
	if found {
		msgID = draft.ID
	} else {
		msgID, createdAt, err = s.insertPendingOutbound(ctx, ts, convo.ID, author, text, meta)
		if err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	extID := whatsappchannel.SimulatorExternalID("ai")
	if err := s.finalizeOutbound(ctx, ts, tenantID, convo.ID, msgID, extID, strutil.TruncateUTF8(text, 280), createdAt); err != nil {
		return err
	}
	sim.Mirror(ctx, realtime.Default(realtime.LogfTo(rlog.Warn)),
		func(ctx context.Context, q string, a ...any) sim.Scanner { return ts.QueryRowContext(ctx, q, a...) },
		nil, tenantID, convo.ID,
		&sim.MirrorMsg{ID: msgID, Direction: "out", Author: author, Type: "text", Text: text, Status: "sent", CreatedAt: createdAt})
	rlog.Info("AI job: simulator outbound stored (no Meta send)", "convoId", convo.ID, "msgId", msgID, "model", meta.Model, "path", meta.Path)
	s.markAiSendDone(ctx, inboundMessageID)
	return nil
}
