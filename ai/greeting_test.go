package ai

import (
	"strings"
	"testing"
)

func TestGreetingReplySelamatMalam(t *testing.T) {
	got := GreetingReply("selamat malam", "formal", "")
	if !strings.Contains(got, "Selamat malam") {
		t.Fatalf("want malam reply, got %q", got)
	}
	if strings.Contains(got, "siang") {
		t.Fatalf("must not contain siang: %q", got)
	}
}

func TestGreetingReplyIgnoresWrongDefaultWhenExplicit(t *testing.T) {
	got := GreetingReply("selamat malam", "formal", "Selamat siang, kak. Ada yang bisa kami bantu?")
	if !strings.Contains(got, "Selamat malam") {
		t.Fatalf("explicit customer greeting wins over template: %q", got)
	}
}

func TestCasualChatOpenerMalamGan(t *testing.T) {
	if !IsCasualChatOpener("malam gan") {
		t.Fatal("malam gan should be casual opener")
	}
	if !IsGreetingLike("malam min") {
		t.Fatal("malam min should be greeting-like")
	}
}

func TestDetectGreetingFeedbackComplaint(t *testing.T) {
	msg := "saya bilang malam kok balasannya siang aih"
	if !IsGreetingFeedback(msg) {
		t.Fatal("expected greeting feedback detection")
	}
	reply := GreetingFeedbackReply(msg, "formal")
	if !strings.Contains(reply, "Selamat malam") {
		t.Fatalf("want malam apology, got %q", reply)
	}
}
