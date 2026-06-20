package whatsapp

import (
	"encoding/json"
	"testing"
)

func TestExtractMediaIDFromRaw_image(t *testing.T) {
	raw := json.RawMessage(`{"id":"wamid.x","type":"image","image":{"id":"media-123","caption":"bukti"}}`)
	got := ExtractMediaIDFromRaw("image", raw)
	if got != "media-123" {
		t.Fatalf("ExtractMediaIDFromRaw() = %q, want media-123", got)
	}
}

func TestExtractMediaIDFromRaw_document(t *testing.T) {
	raw := json.RawMessage(`{"type":"document","document":{"id":"doc-9","filename":"nota.pdf"}}`)
	got := ExtractMediaIDFromRaw("document", raw)
	if got != "doc-9" {
		t.Fatalf("ExtractMediaIDFromRaw() = %q, want doc-9", got)
	}
}

func TestExtractMediaIDFromRaw_empty(t *testing.T) {
	if got := ExtractMediaIDFromRaw("text", nil); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestInboundMessagePreview_image(t *testing.T) {
	got := InboundMessagePreview("image", "")
	if got != "📷 Gambar" {
		t.Fatalf("preview = %q", got)
	}
	got = InboundMessagePreview("image", "bukti transfer")
	if got != "📷 bukti transfer" {
		t.Fatalf("preview with caption = %q", got)
	}
}
