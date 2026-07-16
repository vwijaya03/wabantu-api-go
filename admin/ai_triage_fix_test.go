package admin

import (
	"encoding/json"
	"testing"

	"encore.app/wabantu/ai"
)

func TestTriageJobCursorFixAttempts(t *testing.T) {
	if got := triageJobCursorFixAttempts(nil); got != 0 {
		t.Fatalf("nil: got %d", got)
	}
	raw, _ := json.Marshal(ai.AnalyzeConversationResult{CursorFixAttempts: 2})
	if got := triageJobCursorFixAttempts(raw); got != 2 {
		t.Fatalf("got %d want 2", got)
	}
}
