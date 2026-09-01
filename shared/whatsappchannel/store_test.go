package whatsappchannel

import (
	"testing"

	"encore.app/wabantu/shared/pii"
)

func TestPrepareWriteEncryptsWhenKeyConfigured(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef"
	w, err := PrepareWrite("Omah Apparel", "+6285730864277", "EAAtest-token", key)
	if err != nil {
		t.Fatal(err)
	}
	if !w.UsePII {
		t.Fatal("expected PII write")
	}
	if w.DisplayName != pii.Placeholder || w.PhoneNumber != pii.Placeholder || w.AccessToken != pii.Placeholder {
		t.Fatalf("plaintext columns should be placeholder: %+v", w)
	}
	if !w.DisplayNameEnc.Valid || !w.PhoneNumberEnc.Valid || !w.PhoneNumberIdx.Valid || !w.AccessTokenEnc.Valid {
		t.Fatalf("encrypted columns empty: %+v", w)
	}
	plain, err := pii.Decrypt(w.AccessTokenEnc.String, key)
	if err != nil || plain != "EAAtest-token" {
		t.Fatalf("decrypt token = %q err=%v", plain, err)
	}
}

func TestPrepareWriteLegacyWithoutKey(t *testing.T) {
	w, err := PrepareWrite("Omah", "+62811", "token", "")
	if err != nil {
		t.Fatal(err)
	}
	if w.UsePII {
		t.Fatal("expected legacy write without key")
	}
	if w.AccessToken != "token" || w.PhoneNumber != "+62811" {
		t.Fatalf("legacy fields: %+v", w)
	}
}
