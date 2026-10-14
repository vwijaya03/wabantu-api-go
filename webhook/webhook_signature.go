package webhook

import (
	"context"
	"fmt"
	"strings"

	"encore.dev/rlog"

	"encore.app/wabantu/whatsapp"
)

type appSecretLookup func(ctx context.Context, phoneNumberID, displayPhone string) (string, error)

// verifyInboundWebhookSignature enforces X-Hub-Signature-256 (HMAC-SHA256 over
// the raw body) on every inbound Meta webhook. The HMAC key is only
// whatsapp_channel.meta_app_secret for that phone_number_id. There is no
// deployment-wide fallback. A missing header, empty channel secret, unknown
// channel, or bad signature is rejected. WebhookVerifyToken is not an HMAC
// key; it only answers the GET hub.verify_token challenge.
// There is intentionally no bypass flag (SEC-02 / AUDIT S3).
func verifyInboundWebhookSignature(
	ctx context.Context,
	body []byte,
	sigHeader string,
	messages []whatsapp.InboundMessage,
	lookup appSecretLookup,
) error {
	if lookup == nil {
		lookup = lookupChannelMetaAppSecret
	}
	sigHeader = strings.TrimSpace(sigHeader)
	if sigHeader == "" {
		rlog.Warn("webhook signature rejected: X-Hub-Signature-256 missing")
		return fmt.Errorf("signature required")
	}

	phoneNumberID := whatsapp.WebhookPhoneNumberID(body)
	if phoneNumberID == "" && len(messages) > 0 {
		phoneNumberID = messages[0].ToPhoneNumberID
	}
	displayPhone := whatsapp.WebhookDisplayPhoneNumber(body)
	if displayPhone == "" && len(messages) > 0 {
		displayPhone = messages[0].ToDisplayPhone
	}

	if phoneNumberID == "" {
		rlog.Warn("webhook signature rejected: no phone_number_id")
		return fmt.Errorf("no app secret")
	}
	s, err := lookup(ctx, phoneNumberID, displayPhone)
	if err != nil {
		rlog.Warn("webhook signature rejected: channel not found", "phoneNumberId", phoneNumberID, "err", err)
		return fmt.Errorf("channel not found")
	}
	appSecret := strings.TrimSpace(s)
	if appSecret == "" {
		rlog.Warn("webhook signature rejected: channel has no meta_app_secret", "phoneNumberId", phoneNumberID)
		return fmt.Errorf("no app secret")
	}
	if !whatsapp.VerifyWebhookSignature(body, sigHeader, appSecret) {
		rlog.Warn("webhook signature verification failed", "phoneNumberId", phoneNumberID)
		return fmt.Errorf("signature invalid")
	}
	return nil
}
