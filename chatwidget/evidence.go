package chatwidget

import (
	"context"

	"encore.app/wabantu/admin"
	iev "encore.app/wabantu/shared/interactionevidence/adapters"
	"encore.app/wabantu/shared/kbcontext"
)

func captureWebChatEvidence(ctx context.Context, tenantID, tenantSchema, sessionID, inboundID, outboundID, clientMsgID, userText, replyText, path string, trace kbcontext.Trace, degraded kbcontext.DegradedMode) {
	ev := iev.FromWebChat(iev.WebChatInput{
		SessionID:       sessionID,
		InboundID:       inboundID,
		OutboundID:      outboundID,
		ClientMessageID: clientMsgID,
		UserText:        userText,
		FinalText:       replyText,
		Path:            path,
		Retrieval:       trace,
		DegradedMode:    degraded,
	})
	if !ev.Valid() {
		return
	}
	_ = admin.IngestTriageIncident(ctx, &admin.IngestTriageIncidentParams{
		TenantID:       tenantID,
		TenantSchema:   tenantSchema,
		Channel:        "web_chat",
		ConversationID: sessionID,
		InboundID:      inboundID,
		OutboundID:     outboundID,
		UserText:       userText,
		ReplyText:      replyText,
		Path:           path,
		Lane:           "chatengine",
	})
}
