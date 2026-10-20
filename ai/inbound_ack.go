package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"encore.dev/rlog"

	"encore.app/wabantu/whatsapp"
)

// Seam (swapped in tests only).
var metaMarkRead = whatsapp.MarkReadWithTyping

const inboundAckBudget = 6 * time.Second

// ackInboundAsync marks the inbound message read and shows the typing indicator, in the
// background. Best-effort: it never blocks or fails the reply, errors are only logged.
func ackInboundAsync(ctx context.Context, job *InboundAIJob) {
	if job == nil {
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				rlog.Warn("inbound ack: panic", "recover", fmt.Sprint(r))
			}
		}()
		ackCtx, cancel := context.WithTimeout(bg, inboundAckBudget)
		defer cancel()
		if err := ackInbound(ackCtx, job); err != nil {
			rlog.Warn("inbound ack failed", "conversationId", job.ConversationID, "inboundId", job.InboundMessageID, "err", err)
		}
	}()
}

func ackInbound(ctx context.Context, job *InboundAIJob) error {
	ts, err := openTenantScope(ctx, job.TenantSchema)
	if err != nil {
		return err
	}
	convo, err := loadConversation(ctx, ts, job.ConversationID)
	if err != nil || convo == nil {
		return err
	}
	if !convo.AIHandled {
		return nil // the AI will not answer: no typing indicator
	}
	channel, err := loadChannel(ctx, ts, convo.ChannelID)
	if err != nil || channel == nil {
		return err
	}
	if route, _ := autoReplyRoute(channel); route != routeMeta {
		return nil // simulator and unusable channels never call Meta
	}
	var wamid string
	row := ts.QueryRowContext(ctx, fmt.Sprintf(`SELECT COALESCE(external_id,'') FROM %s WHERE id = $1`, ts.T("message")), job.InboundMessageID)
	if err := row.Scan(&wamid); err != nil {
		return err
	}
	if strings.TrimSpace(wamid) == "" {
		return nil
	}
	return metaMarkRead(ctx, *channel.AccessToken, *channel.MetaPhoneNumberID, wamid)
}
