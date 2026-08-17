package webhook

import (
	"context"
	"fmt"

	"encore.dev/rlog"

	"encore.app/wabantu/whatsapp"
)

type appSecretLookup func(ctx context.Context, phoneNumberID string) (string, error)

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

	phoneNumberID := whatsapp.WebhookPhoneNumberID(body)
	if phoneNumberID == "" && len(messages) > 0 {
		phoneNumberID = messages[0].ToPhoneNumberID
	}
	if phoneNumberID == "" {
		if sigHeader != "" {
			rlog.Warn("webhook signature present but phone_number_id missing in payload")
			return fmt.Errorf("phone_number_id missing")
		}
		return nil
	}

	appSecret, err := lookup(ctx, phoneNumberID)
	if err != nil {
		rlog.Warn("webhook signature rejected: channel not found", "phoneNumberId", phoneNumberID, "err", err)
		return fmt.Errorf("channel not found")
	}
	if appSecret == "" {
		if sigHeader != "" {
			rlog.Warn("webhook signature rejected: no app secret configured", "phoneNumberId", phoneNumberID)
			return fmt.Errorf("no app secret")
		}
		return nil
	}

	if sigHeader == "" {
		rlog.Warn("webhook signature required but missing", "phoneNumberId", phoneNumberID)
		return fmt.Errorf("signature required")
	}
	if !whatsapp.VerifyWebhookSignature(body, sigHeader, appSecret) {
		rlog.Warn("webhook signature verification failed", "phoneNumberId", phoneNumberID)
		return fmt.Errorf("signature invalid")
	}
	return nil
}
