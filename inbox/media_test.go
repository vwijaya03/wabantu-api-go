package inbox

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestMessageMediaAPIPath(t *testing.T) {
	got := messageMediaAPIPath("msg-uuid-1")
	want := "/inbox/messages/msg-uuid-1/media"
	if got != want {
		t.Fatalf("messageMediaAPIPath() = %q, want %q", got, want)
	}
}

func TestDefaultMimeForMessageType(t *testing.T) {
	cases := []struct {
		msgType string
		want    string
	}{
		{"image", "image/jpeg"},
		{"sticker", "image/jpeg"},
		{"video", "video/mp4"},
		{"audio", "audio/ogg"},
		{"document", "application/octet-stream"},
		{"unknown", "application/octet-stream"},
	}
	for _, tc := range cases {
		if got := defaultMimeForMessageType(tc.msgType); got != tc.want {
			t.Fatalf("defaultMimeForMessageType(%q) = %q, want %q", tc.msgType, got, tc.want)
		}
	}
}

func TestEnrichMessageMedia_image(t *testing.T) {
	meta := json.RawMessage(`{"type":"image","image":{"id":"media-123","caption":"cek"}}`)
	m := &MessageItem{ID: "m1", Type: "image"}
	enrichMessageMedia(m, meta)
	if m.Media == nil {
		t.Fatal("expected media info")
	}
	if m.Media.URL != "/inbox/messages/m1/media" {
		t.Fatalf("url = %q", m.Media.URL)
	}
	if m.Media.MimeType != "image/jpeg" {
		t.Fatalf("mime = %q", m.Media.MimeType)
	}
}

func TestEnrichMessageMedia_skipsText(t *testing.T) {
	m := &MessageItem{ID: "m1", Type: "text"}
	enrichMessageMedia(m, json.RawMessage(`{"type":"text","text":{"body":"halo"}}`))
	if m.Media != nil {
		t.Fatal("text message should not have media")
	}
}

func TestEnrichMessageMedia_skipsWithoutMediaID(t *testing.T) {
	m := &MessageItem{ID: "m1", Type: "image"}
	enrichMessageMedia(m, json.RawMessage(`{"type":"image","image":{}}`))
	if m.Media != nil {
		t.Fatal("image without media id should not have media url")
	}
}

func TestInboxMediaCacheKey(t *testing.T) {
	got := inboxMediaCacheKey("t_demo", "msg-1")
	want := "inbox:media:t_demo:msg-1"
	if got != want {
		t.Fatalf("cache key = %q, want %q", got, want)
	}
}

func TestMessageIDFromMediaPath_pathValue(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/inbox/messages/abc-123/media", nil)
	req.SetPathValue("messageId", "abc-123")
	if got := messageIDFromMediaPath(req); got != "abc-123" {
		t.Fatalf("got %q", got)
	}
}

func TestMessageIDFromMediaPath_urlFallback(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/inbox/messages/fallback-id/media", nil)
	if got := messageIDFromMediaPath(req); got != "fallback-id" {
		t.Fatalf("got %q", got)
	}
}

func TestCachedInboxMediaRoundTrip(t *testing.T) {
	entry := cachedInboxMedia{MimeType: "image/png", Data: []byte{1, 2, 3}}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var decoded cachedInboxMedia
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.MimeType != entry.MimeType || len(decoded.Data) != 3 {
		t.Fatalf("decoded = %+v", decoded)
	}
}
