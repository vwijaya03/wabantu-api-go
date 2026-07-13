package ai

import (
	"encoding/json"
	"testing"
	"time"
)

func TestValidateLLMScanWindow(t *testing.T) {
	from := parseTime("2026-07-13T10:00:00Z")
	to := parseTime("2026-07-13T11:00:00Z")
	if err := ValidateLLMScanWindow(from, to); err != nil {
		t.Fatalf("valid window: %v", err)
	}
	if err := ValidateLLMScanWindow(to, from); err == nil {
		t.Fatal("expected error when to before from")
	}
	longTo := from.Add(7 * triageLLMMaxWindow)
	if err := ValidateLLMScanWindow(from, longTo); err == nil {
		t.Fatal("expected error for window too long")
	}
}

func TestExtractAITurnsForConversationWindow(t *testing.T) {
	meta, _ := json.Marshal(AiReplyMeta{Path: PathGreeting})
	messages := []windowMessage{
		{TriageMessage: TriageMessage{ID: "in-1", Direction: "in", Body: "halo", CreatedAt: parseTime("2026-07-13T10:00:00Z")}, ConversationID: "c1"},
		{TriageMessage: TriageMessage{ID: "out-1", Direction: "out", Body: "Halo kak!", Metadata: meta, CreatedAt: parseTime("2026-07-13T10:00:01Z")}, ConversationID: "c1"},
	}
	turns := extractAITurnsForConversationWindow(messages, "c1", 10)
	if len(turns) != 1 {
		t.Fatalf("turns len = %d want 1", len(turns))
	}
	if turns[0].Path != PathGreeting || turns[0].ReplyText != "Halo kak!" {
		t.Fatalf("unexpected turn: %+v", turns[0])
	}
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
