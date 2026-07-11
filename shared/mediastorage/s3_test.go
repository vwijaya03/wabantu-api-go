package mediastorage

import (
	"context"
	"testing"
)

func TestConfigured_emptySecrets(t *testing.T) {
	if Configured() {
		t.Fatal("expected Configured() false when secrets are empty in test env")
	}
}

func TestPutGetDelete_notConfigured(t *testing.T) {
	ctx := context.Background()
	if err := Put(ctx, "key", []byte("data"), "text/plain"); err == nil {
		t.Fatal("expected Put error when s3 not configured")
	}
	if _, _, err := Get(ctx, "key"); err == nil {
		t.Fatal("expected Get error when s3 not configured")
	}
	if err := Delete(ctx, "key"); err == nil {
		t.Fatal("expected Delete error when s3 not configured")
	}
}

func TestBuildInboxMediaKey(t *testing.T) {
	data := []byte("hello media")
	key := BuildInboxMediaKey("t_demo", "msg-uuid-1", data, "image/jpeg")
	wantPrefix := "t_demo/inbox/msg-uuid-1/"
	if len(key) <= len(wantPrefix) {
		t.Fatalf("key too short: %q", key)
	}
	if key[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("key prefix = %q, want %q*", key, wantPrefix)
	}
	if key[len(key)-4:] != ".jpg" {
		t.Fatalf("key suffix = %q, want .jpg", key[len(key)-4:])
	}

	key2 := BuildInboxMediaKey("t_demo", "msg-uuid-1", data, "image/jpeg")
	if key != key2 {
		t.Fatalf("key not deterministic: %q vs %q", key, key2)
	}
}

func TestExtFromMIME(t *testing.T) {
	cases := []struct {
		mime string
		want string
	}{
		{"image/jpeg", "jpg"},
		{"image/png", "png"},
		{"video/mp4", "mp4"},
		{"audio/ogg", "ogg"},
		{"application/pdf", "pdf"},
		{"image/webp", "webp"},
		{"unknown/type", "bin"},
	}
	for _, tc := range cases {
		if got := extFromMIME(tc.mime); got != tc.want {
			t.Fatalf("extFromMIME(%q) = %q, want %q", tc.mime, got, tc.want)
		}
	}
}
