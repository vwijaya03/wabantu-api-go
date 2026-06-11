package whatsapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifyWebhookSignatureValid(t *testing.T) {
	body := []byte(`{"test":true}`)
	secret := "test-app-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !VerifyWebhookSignature(body, sig, secret) {
		t.Fatal("expected valid signature")
	}
}

func TestVerifyWebhookSignatureInvalid(t *testing.T) {
	if VerifyWebhookSignature([]byte("x"), "sha256=bad", "secret") {
		t.Fatal("expected invalid signature")
	}
}

func TestWebhookPhoneNumberIDFromStatusPayload(t *testing.T) {
	payload := []byte(`{
	  "object":"whatsapp_business_account",
	  "entry":[{"changes":[{"value":{
	    "metadata":{"phone_number_id":"123456","display_phone_number":"15550001"},
	    "statuses":[{"id":"wamid.x","status":"delivered"}]
	  }}]}]
	}`)
	if got := WebhookPhoneNumberID(payload); got != "123456" {
		t.Fatalf("phone_number_id=%q want 123456", got)
	}
	if msgs := ParseWebhook(payload); len(msgs) != 0 {
		t.Fatalf("expected no inbound messages from status webhook, got %d", len(msgs))
	}
}
