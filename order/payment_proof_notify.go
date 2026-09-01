package order

import (
	"context"
	"fmt"
	"strings"

	"encore.dev/rlog"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/strutil"
	"encore.app/wabantu/tenant"
	"encore.app/wabantu/whatsapp"
)

func sendPaymentProofConversationMessage(ctx context.Context, tenantSchema, conversationID, text string) error {
	text = strings.TrimSpace(text)
	if conversationID == "" || text == "" {
		return nil
	}
	if err := tenant.PrepareTenantAccess(ctx, tenantSchema); err != nil {
		return err
	}
	sch := appdb.SchemaSQL{Schema: tenantSchema}
	pool := db.Stdlib()

	var contactID, channelID string
	err := pool.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT contact_id::text, channel_id::text FROM %s WHERE id = $1::uuid`, sch.T("conversation")),
		conversationID,
	).Scan(&contactID, &channelID)
	if err != nil {
		return fmt.Errorf("conversation not found: %w", err)
	}

	var contactPhone string
	err = pool.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COALESCE(phone_number, '') FROM %s WHERE id = $1::uuid`, sch.T("contact")),
		contactID,
	).Scan(&contactPhone)
	if err != nil {
		return fmt.Errorf("contact not found: %w", err)
	}

	var chStatus, chProvider string
	var tokenEnc, tokenLegacy string
	var chMetaPhoneNumberID *string
	err = pool.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT status,
		       COALESCE(access_token_enc, ''), COALESCE(access_token, ''),
		       provider, meta_phone_number_id
		 FROM %s WHERE id = $1::uuid`, sch.T("whatsapp_channel")),
		channelID,
	).Scan(&chStatus, &tokenEnc, &tokenLegacy, &chProvider, &chMetaPhoneNumberID)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}
	chAccessToken, err := pii.DecryptOrLegacy(tokenEnc, tokenLegacy, strings.TrimSpace(secrets.DataEncryptionKey))
	if err != nil {
		return fmt.Errorf("decrypt channel token: %w", err)
	}
	if chStatus != "connected" || chAccessToken == "" || chProvider != "meta_cloud" {
		return fmt.Errorf("whatsapp channel not ready")
	}
	if chMetaPhoneNumberID == nil || strings.TrimSpace(*chMetaPhoneNumberID) == "" {
		return fmt.Errorf("whatsapp channel missing meta_phone_number_id")
	}

	phone := whatsapp.NormalizeRecipient(contactPhone)
	if phone == "" {
		return fmt.Errorf("invalid contact phone")
	}

	extID, err := whatsapp.SendText(ctx, chAccessToken, *chMetaPhoneNumberID, phone, text)
	if err != nil {
		rlog.Warn("payment proof notify SendText failed", "err", err, "conversationId", conversationID)
		return err
	}

	_, err = pool.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (conversation_id, external_id, direction, author, type, body, metadata, status)
		VALUES ($1::uuid, $2, 'out', 'system', 'text', $3, '{}'::jsonb, 'sent')`,
		sch.T("message")), conversationID, extID, text)
	if err != nil {
		return fmt.Errorf("save message: %w", err)
	}

	preview := strutil.TruncateUTF8(text, 280)
	_, _ = pool.ExecContext(ctx, fmt.Sprintf(
		`UPDATE %s SET last_message_at = NOW(), last_message_preview = $1, status = 'open' WHERE id = $2::uuid`,
		sch.T("conversation")), preview, conversationID)
	return nil
}
