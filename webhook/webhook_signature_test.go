package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"encore.app/wabantu/whatsapp"
)

func signWebhookBody(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyInboundWebhookSignatureRequiresSigWhenSecretConfigured(t *testing.T) {
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"changes":[{"value":{"metadata":{"phone_number_id":"123"}}}]}]}`)
	lookup := func(context.Context, string) (string, error) {
		return "secret", nil
	}
	if err := verifyInboundWebhookSignature(context.Background(), body, "", nil, lookup); err == nil {
		t.Fatal("expected error when signature missing but app secret configured")
	}
}

func TestVerifyInboundWebhookSignatureValidWhenSecretConfigured(t *testing.T) {
	secret := "test-app-secret"
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"changes":[{"value":{"metadata":{"phone_number_id":"123"}}}]}]}`)
	sig := signWebhookBody(body, secret)
	lookup := func(context.Context, string) (string, error) {
		return secret, nil
	}
	if err := verifyInboundWebhookSignature(context.Background(), body, sig, nil, lookup); err != nil {
		t.Fatalf("expected valid signature, got %v", err)
	}
}

func TestVerifyInboundWebhookSignatureAllowsMissingSigWithoutSecret(t *testing.T) {
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"changes":[{"value":{"metadata":{"phone_number_id":"123"}}}]}]}`)
	lookup := func(context.Context, string) (string, error) {
		return "", nil
	}
	if err := verifyInboundWebhookSignature(context.Background(), body, "", nil, lookup); err != nil {
		t.Fatalf("expected legacy channel without secret to pass, got %v", err)
	}
}

func TestVerifyInboundWebhookSignatureUsesMessagePhoneNumberID(t *testing.T) {
	secret := "test-app-secret"
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"changes":[{"value":{"messages":[{"from":"1"}]}}]}]}`)
	messages := []whatsapp.InboundMessage{{ToPhoneNumberID: "999"}}
	sig := signWebhookBody(body, secret)
	lookup := func(_ context.Context, phoneNumberID string) (string, error) {
		if phoneNumberID != "999" {
			t.Fatalf("phoneNumberID=%q want 999", phoneNumberID)
		}
		return secret, nil
	}
	if err := verifyInboundWebhookSignature(context.Background(), body, sig, messages, lookup); err != nil {
		t.Fatalf("expected valid signature via message phone id, got %v", err)
	}
}
