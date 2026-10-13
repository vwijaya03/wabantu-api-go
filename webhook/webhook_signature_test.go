package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	lookup := func(context.Context, string, string) (string, error) {
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
	lookup := func(context.Context, string, string) (string, error) {
		return secret, nil
	}
	if err := verifyInboundWebhookSignature(context.Background(), body, sig, nil, lookup); err != nil {
		t.Fatalf("expected valid signature, got %v", err)
	}
}

func TestVerifyInboundWebhookSignaturePassesDisplayPhoneToLookup(t *testing.T) {
	secret := "test-app-secret"
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"changes":[{"value":{"metadata":{"phone_number_id":"123","display_phone_number":"6285730864277"}}}]}]}`)
	sig := signWebhookBody(body, secret)
	lookup := func(_ context.Context, phoneNumberID, displayPhone string) (string, error) {
		if phoneNumberID != "123" {
			t.Fatalf("phoneNumberID=%q want 123", phoneNumberID)
		}
		if displayPhone != "6285730864277" {
			t.Fatalf("displayPhone=%q want 6285730864277", displayPhone)
		}
		return secret, nil
	}
	if err := verifyInboundWebhookSignature(context.Background(), body, sig, nil, lookup); err != nil {
		t.Fatalf("expected valid signature with display phone, got %v", err)
	}
}

func withGlobalSecret(t *testing.T, v string) {
	t.Helper()
	old := globalMetaAppSecret
	globalMetaAppSecret = func() string { return v }
	t.Cleanup(func() { globalMetaAppSecret = old })
}

const testBody = `{"object":"whatsapp_business_account","entry":[{"changes":[{"value":{"metadata":{"phone_number_id":"123"}}}]}]}`

func TestVerifyInboundWebhookSignatureRejectsMissingSigWithoutSecret(t *testing.T) {
	withGlobalSecret(t, "")
	lookup := func(context.Context, string, string) (string, error) { return "", nil }
	if err := verifyInboundWebhookSignature(context.Background(), []byte(testBody), "", nil, lookup); err == nil {
		t.Fatal("expected unsigned request to be rejected even when channel has no secret")
	}
}

func TestVerifyInboundWebhookSignatureRejectsWhenNoSecretAnywhere(t *testing.T) {
	withGlobalSecret(t, "")
	body := []byte(testBody)
	lookup := func(context.Context, string, string) (string, error) { return "", nil }
	if err := verifyInboundWebhookSignature(context.Background(), body, signWebhookBody(body, "x"), nil, lookup); err == nil {
		t.Fatal("expected rejection when no app secret configured")
	}
}

func TestVerifyInboundWebhookSignatureRejectsMissingPhoneIDUnsigned(t *testing.T) {
	withGlobalSecret(t, "")
	body := []byte(`{"object":"whatsapp_business_account","entry":[]}`)
	if err := verifyInboundWebhookSignature(context.Background(), body, "", nil, nil); err == nil {
		t.Fatal("expected payload without phone_number_id and signature to be rejected")
	}
}

func TestVerifyInboundWebhookSignatureGlobalSecretFallback(t *testing.T) {
	withGlobalSecret(t, "global-secret")
	body := []byte(testBody)
	lookup := func(context.Context, string, string) (string, error) { return "", nil }
	if err := verifyInboundWebhookSignature(context.Background(), body, signWebhookBody(body, "global-secret"), nil, lookup); err != nil {
		t.Fatalf("expected global secret to verify, got %v", err)
	}
	if err := verifyInboundWebhookSignature(context.Background(), body, signWebhookBody(body, "wrong"), nil, lookup); err == nil {
		t.Fatal("expected wrong signature to be rejected")
	}
}

func TestVerifyInboundWebhookSignatureRejectsInvalidAndMalformed(t *testing.T) {
	withGlobalSecret(t, "")
	body := []byte(testBody)
	lookup := func(context.Context, string, string) (string, error) { return "s", nil }
	for _, sig := range []string{"sha256=deadbeef", "garbage", signWebhookBody([]byte("other"), "s"), "sha1=" + signWebhookBody(body, "s")[7:]} {
		if err := verifyInboundWebhookSignature(context.Background(), body, sig, nil, lookup); err == nil {
			t.Fatalf("expected rejection for sig %q", sig)
		}
	}
}

func TestVerifyInboundWebhookSignatureRejectsUnknownChannel(t *testing.T) {
	withGlobalSecret(t, "global-secret")
	body := []byte(testBody)
	lookup := func(context.Context, string, string) (string, error) { return "", fmt.Errorf("nope") }
	if err := verifyInboundWebhookSignature(context.Background(), body, signWebhookBody(body, "global-secret"), nil, lookup); err == nil {
		t.Fatal("expected unknown channel to be rejected")
	}
}

func TestVerifyInboundWebhookSignatureUsesMessagePhoneNumberID(t *testing.T) {
	secret := "test-app-secret"
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"changes":[{"value":{"messages":[{"from":"1"}]}}]}]}`)
	messages := []whatsapp.InboundMessage{{ToPhoneNumberID: "999"}}
	sig := signWebhookBody(body, secret)
	lookup := func(_ context.Context, phoneNumberID, _ string) (string, error) {
		if phoneNumberID != "999" {
			t.Fatalf("phoneNumberID=%q want 999", phoneNumberID)
		}
		return secret, nil
	}
	if err := verifyInboundWebhookSignature(context.Background(), body, sig, messages, lookup); err != nil {
		t.Fatalf("expected valid signature via message phone id, got %v", err)
	}
}
