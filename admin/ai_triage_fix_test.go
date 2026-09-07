package admin

import (
	"encoding/json"
	"strings"
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

func TestForensicDuplicateMessage(t *testing.T) {
	id := "0aa8d9f4-62b2-43ab-ae3d-0c0fbd61bb1d"
	msg := forensicDuplicateMessage(id)
	if !strings.Contains(msg, id) {
		t.Fatalf("message should include job id: %s", msg)
	}
	if !strings.Contains(msg, "force=true") {
		t.Fatalf("message should mention force: %s", msg)
	}
	if !strings.Contains(msg, "Verifikasi fix") {
		t.Fatalf("message should mention verify: %s", msg)
	}
}

func TestTriageJobCanVerify(t *testing.T) {
	if !triageJobCanVerify(triageJobStatusPRReady) || !triageJobCanVerify(triageJobStatusPRReadyNeedsFix) {
		t.Fatal("pr_ready statuses should be verifiable")
	}
	if !triageJobCanVerify(triageJobStatusVerified) {
		t.Fatal("verified jobs can re-verify")
	}
	if triageJobCanVerify(triageJobStatusPending) || triageJobCanVerify(triageJobStatusRunning) {
		t.Fatal("in-flight forensic jobs must not verify")
	}
}
