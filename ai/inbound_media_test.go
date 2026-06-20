package ai

import "testing"

func TestInboundTextForAutoReply_text(t *testing.T) {
	got, ok := inboundTextForAutoReply("text", "ada stok kaos L?")
	if !ok || got != "ada stok kaos L?" {
		t.Fatalf("text: got %q ok=%v", got, ok)
	}
}

func TestInboundTextForAutoReply_textEmpty(t *testing.T) {
	_, ok := inboundTextForAutoReply("text", "   ")
	if ok {
		t.Fatal("expected empty text to be rejected")
	}
}

func TestInboundTextForAutoReply_imageWithCaption(t *testing.T) {
	got, ok := inboundTextForAutoReply("image", "kamu punya barang ini gak min ?")
	if !ok || got != "kamu punya barang ini gak min ?" {
		t.Fatalf("image+caption: got %q ok=%v", got, ok)
	}
}

func TestInboundTextForAutoReply_imageWithoutCaption(t *testing.T) {
	_, ok := inboundTextForAutoReply("image", "")
	if ok {
		t.Fatal("expected image without caption to be rejected")
	}
}

func TestInboundTextForAutoReply_videoWithCaption(t *testing.T) {
	got, ok := inboundTextForAutoReply("video", "cek ini min")
	if !ok || got != "cek ini min" {
		t.Fatalf("video+caption: got %q ok=%v", got, ok)
	}
}

func TestInboundTextForAutoReply_documentWithCaption(t *testing.T) {
	got, ok := inboundTextForAutoReply("document", "invoice bulan lalu")
	if !ok || got != "invoice bulan lalu" {
		t.Fatalf("document+caption: got %q ok=%v", got, ok)
	}
}

func TestInboundTextForAutoReply_audioSkipped(t *testing.T) {
	_, ok := inboundTextForAutoReply("audio", "transkrip")
	if ok {
		t.Fatal("audio should not use caption path in v1")
	}
}

func TestInboundTextForAutoReply_stickerSkipped(t *testing.T) {
	_, ok := inboundTextForAutoReply("sticker", "")
	if ok {
		t.Fatal("sticker without caption should be rejected")
	}
}

func TestIsMediaTypeWithOptionalCaption(t *testing.T) {
	if !isMediaTypeWithOptionalCaption("image") {
		t.Fatal("image should be media with optional caption")
	}
	if isMediaTypeWithOptionalCaption("text") {
		t.Fatal("text is not a media type")
	}
}
