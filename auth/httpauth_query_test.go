package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAccessTokenQueryOnlyOnInboxStream(t *testing.T) {
	stream := httptest.NewRequest(http.MethodGet, "/api/v1/inbox/stream?access_token=stream-tok", nil)
	if got := extractBearerOrCookie(stream); got != "stream-tok" {
		t.Fatalf("inbox stream: got %q", got)
	}
	trailing := httptest.NewRequest(http.MethodGet, "/api/v1/inbox/stream/?access_token=stream-tok", nil)
	if got := extractBearerOrCookie(trailing); got != "stream-tok" {
		t.Fatalf("trailing slash: got %q", got)
	}

	blocked := []string{
		"/api/v1/auth/me?access_token=leak",
		"/api/v1/orders?access_token=leak",
		"/api/v1/inbox/stream-extra?access_token=leak",
		"/api/v1/platform-admin/me?access_token=leak",
	}
	for _, raw := range blocked {
		req := httptest.NewRequest(http.MethodGet, raw, nil)
		if got := extractBearerOrCookie(req); got != "" {
			t.Errorf("%s accepted query token %q", raw, got)
		}
	}

	bearer := httptest.NewRequest(http.MethodGet, "/api/v1/orders?access_token=leak", nil)
	bearer.Header.Set("Authorization", "Bearer header-tok")
	if got := extractBearerOrCookie(bearer); got != "header-tok" {
		t.Fatalf("bearer on non-stream: got %q", got)
	}
}
