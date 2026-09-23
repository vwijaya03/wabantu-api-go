package chatwidget

import "testing"

func TestParseStreamPath(t *testing.T) {
	p := parseStreamPath("/api/v1/public/chat/omah-apparel/sessions/abc-123/stream")
	if p.TenantSlug != "omah-apparel" || p.SessionID != "abc-123" {
		t.Fatalf("got %+v", p)
	}
}
