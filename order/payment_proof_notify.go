package order

import (
	"context"
	"fmt"
	"strings"

	"encore.dev/rlog"

	"encore.app/wabantu/shared/strutil"
	"encore.app/wabantu/tenant"
	"encore.app/wabantu/whatsapp"
)

func sendPaymentProofConversationMessage(ctx context.Context, tenantSchema, conversationID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" || conversationID == "" {
		return nil
	}
	conn, err := tenant.TenantConn(ctx, tenantSchema)
	if err != nil {
		return err
	}
	defer conn.Close()

	var contactID, channelID string
	err = conn.QueryRowContext(ctx,
		`SELECT contact_id::text, channel_id::text FROM conversation WHERE id = $1::uuid`, conversationID,
	).Scan(&contactID, &channelID)
	if err != nil {
		return fmt.Errorf("conversation not found: %w", err)
	}

	var contactPhone string
	err = conn.QueryRowContext(ctx,
		`SELECT COALESCE(phone_number, '') FROM contact WHERE id = $1::uuid`, contactID,
	).Scan(&contactPhone)
	if err != nil {
		return fmt.Errorf("contact not found: %w", err)
	}

	var chStatus, chAccessToken, chProvider string
	var chMetaPhoneNumberID *string
	err = conn.QueryRowContext(ctx,
		`SELECT status, COALESCE(access_token,''), provider, meta_phone_number_id
		 FROM whatsapp_channel WHERE id = $1::uuid`, channelID,
	).Scan(&chStatus, &chAccessToken, &chProvider, &chMetaPhoneNumberID)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
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

	_, err = conn.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO "%s".message (conversation_id, external_id, direction, author, type, body, metadata, status)
		VALUES ($1::uuid, $2, 'out', 'system', 'text', $3, '{}'::jsonb, 'sent')`,
		tenantSchema), conversationID, extID, text)
	if err != nil {
		return fmt.Errorf("save message: %w", err)
	}

	preview := strutil.TruncateUTF8(text, 280)
	_, _ = conn.ExecContext(ctx,
		`UPDATE conversation SET last_message_at = NOW(), last_message_preview = $1, status = 'open' WHERE id = $2::uuid`,
		preview, conversationID)
	return nil
}
