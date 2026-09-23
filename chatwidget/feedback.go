package chatwidget

import (
	"context"
	"strings"

	"encore.app/wabantu/admin"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/publictenant"
)

type NegativeFeedbackRequest struct {
	MessageID    string `json:"messageId"`
	VisitorToken string `json:"visitorToken"`
	Reason       string `json:"reason,omitempty"`
}

//encore:api public method=POST path=/api/v1/public/chat/:tenantSlug/sessions/:sessionId/feedback
func PostNegativeFeedback(ctx context.Context, tenantSlug, sessionId string, req *NegativeFeedbackRequest) error {
	_, err := publictenant.RunPublicTenant(ctx, tenantSlug, resolveTenantBySlug, func(ref publictenant.TenantRef) (struct{}, error) {
		token := strings.TrimSpace(req.VisitorToken)
		if !verifyVisitorToken(secrets.JWTSecret, ref.Slug, sessionId, token) {
			return struct{}{}, appErrs.Unauthenticated("token visitor tidak valid")
		}
		msgID := strings.TrimSpace(req.MessageID)
		if msgID == "" {
			return struct{}{}, appErrs.BadRequest("messageId wajib")
		}

		ts, err := openTenant(ctx, ref.TenantSchema)
		if err != nil {
			return struct{}{}, err
		}
		var body string
		err = ts.QueryRowContext(ctx, `
			SELECT body FROM `+ts.T("web_chat_message")+`
			WHERE id = $1::uuid AND session_id = $2::uuid AND role = 'assistant'`,
			msgID, sessionId).Scan(&body)
		if err != nil {
			return struct{}{}, appErrs.NotFound("pesan tidak ditemukan")
		}

		reason := strings.TrimSpace(req.Reason)
		if reason == "" {
			reason = "negative_feedback"
		}
		_ = admin.IngestTriageIncident(ctx, &admin.IngestTriageIncidentParams{
			TenantID:       ref.TenantID,
			TenantSchema:   ref.TenantSchema,
			Channel:        "web_chat",
			ConversationID: sessionId,
			OutboundID:     msgID,
			ReplyText:      body,
			Path:           "negative_feedback:" + reason,
			Lane:           "chatengine",
		})
		return struct{}{}, nil
	})
	return err
}
